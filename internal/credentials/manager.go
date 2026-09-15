package credentials

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Thunder-Compute/thunder-cli/internal/clierr"
	"github.com/Thunder-Compute/thunder-cli/pkg/types"
)

const defaultRefreshSkew = 30 * time.Second
const refreshTimeout = 30 * time.Second

var (
	ErrReauthenticationRequired = errors.New("saved credential requires reauthentication; run 'tnr login' again")
	ErrCredentialChanged        = clierr.New("saved login changed while this command was running; rerun the command to use the current account")
)

// Manager supplies current access tokens and serializes rotating refresh-token use across both goroutines and CLI processes. It accepts rotations only for the saved login captured when the command began.
type Manager struct {
	path         string
	credentialID string
	oauth        oauthClient
	now          func() time.Time
	refreshSkew  time.Duration
	writeConfig  func(string, Config) error
}

func NewManager(config *Config) (*Manager, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, errors.New("credential manager requires the command's initial credential")
	}
	return newManager(path, nil, config.CredentialID), nil
}

func newManager(path string, client *http.Client, credentialID string) *Manager {
	return &Manager{
		path:         path,
		credentialID: credentialID,
		oauth:        newOAuthClient(client),
		now:          time.Now,
		refreshSkew:  defaultRefreshSkew,
		writeConfig:  writeAtomic,
	}
}

// Token implements api.TokenSource.
func (m *Manager) Token(ctx context.Context) (string, error) {
	config, err := loadStored(m.path)
	if err != nil {
		return "", clierr.Mark(fmt.Errorf("load saved credentials: %w", err))
	}
	if err := m.requireSameCredential(config); err != nil {
		return "", clierr.Mark(err)
	}
	if config.CredentialSource != SourceBrowser || config.CredentialType != types.CredentialTypeCLIOAuth {
		return config.Token, nil
	}
	if _, err := requirePinnedOAuthConfig(config); err != nil {
		return "", clierr.Mark(err)
	}
	if config.ExpiresAt == nil || config.ExpiresAt.After(m.now().Add(m.refreshSkew)) {
		return config.Token, nil
	}
	token, err := m.refreshLocked(ctx, "")
	return token, clierr.Mark(err)
}

// Refresh implements api.RefreshingTokenSource. rejectedToken is the exact token that received a 401; if another process has already rotated the same saved login, the newly persisted token is returned without another refresh.
func (m *Manager) Refresh(ctx context.Context, rejectedToken string) (string, error) {
	token, err := m.refreshLocked(ctx, rejectedToken)
	return token, clierr.Mark(err)
}

func (m *Manager) refreshLocked(ctx context.Context, rejectedToken string) (string, error) {
	var token string
	err := withFileLock(ctx, m.path, func() error {
		config, err := loadStored(m.path)
		if err != nil {
			return fmt.Errorf("reload saved credentials: %w", err)
		}
		if err := m.requireSameCredential(config); err != nil {
			return err
		}
		if config.CredentialSource != SourceBrowser || config.CredentialType != types.CredentialTypeCLIOAuth {
			return errors.New("saved credential is not a refreshable Thunder CLI OAuth login")
		}
		oauthConfig, err := requirePinnedOAuthConfig(config)
		if err != nil {
			return err
		}
		if config.RefreshToken == "" {
			return fmt.Errorf("%w: saved CLI refresh token is missing", ErrReauthenticationRequired)
		}
		if rejectedToken != "" && config.Token != rejectedToken {
			token = config.Token
			return nil
		}
		if rejectedToken == "" && config.ExpiresAt != nil && config.ExpiresAt.After(m.now().Add(m.refreshSkew)) {
			token = config.Token
			return nil
		}

		if err := ctx.Err(); err != nil {
			return err
		}
		// Once rotation starts, caller cancellation must not discard the replacement token before it is saved.
		refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
		defer cancel()
		response, err := m.oauth.Refresh(refreshCtx, *oauthConfig, config.RefreshToken)
		if err != nil {
			var providerErr *ProviderError
			if errors.As(err, &providerErr) && providerErr.InvalidGrant() {
				return fmt.Errorf("%w: CLI login has expired or was revoked: %w", ErrReauthenticationRequired, err)
			}
			return fmt.Errorf("refresh CLI access token: %w", err)
		}

		config.Token = response.AccessToken
		config.RefreshToken = response.RefreshToken
		expiresAt := m.now().UTC().Add(time.Duration(response.ExpiresIn) * time.Second)
		config.ExpiresAt = &expiresAt
		if err := m.writeConfig(m.path, *config); err != nil {
			return fmt.Errorf("the OAuth provider rotated the refresh token but the new credential could not be saved; run 'tnr login' again: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		token = config.Token
		return nil
	})
	return token, err
}

func (m *Manager) Delete(ctx context.Context) error {
	return withFileLock(ctx, m.path, func() error {
		config, err := loadStored(m.path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load saved credentials: %w", err)
		}
		if err := m.requireSameCredential(config); err != nil {
			return err
		}
		if err := os.Remove(m.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	})
}

func (m *Manager) requireSameCredential(config *Config) error {
	if m.credentialID == "" {
		return fmt.Errorf("%w: saved CLI login is missing its credential identity", ErrReauthenticationRequired)
	}
	if config.CredentialID != m.credentialID {
		return ErrCredentialChanged
	}
	return nil
}

func requirePinnedOAuthConfig(config *Config) (*OAuthConfig, error) {
	if config.OAuthConfig == nil {
		return nil, fmt.Errorf("%w: saved CLI OAuth login is missing its pinned configuration", ErrReauthenticationRequired)
	}
	if err := config.OAuthConfig.Validate(); err != nil {
		return nil, fmt.Errorf("%w: saved CLI OAuth login has an invalid pinned configuration: %w", ErrReauthenticationRequired, err)
	}
	return config.OAuthConfig, nil
}
