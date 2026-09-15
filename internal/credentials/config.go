package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Thunder-Compute/thunder-cli/pkg/types"
	"github.com/Thunder-Compute/thunder-cli/utils"
)

const DefaultAPIURL = "https://api.thundercompute.com:8443"

// Source records how the CLI obtained a credential. It is local provenance
// only and must never be used by the API as an authorization input.
type Source string

const (
	SourceBrowser       Source = "browser"
	SourceProvidedToken Source = "provided_token"
	SourceEnvironment   Source = "environment"
)

// Config is the backward-compatible on-disk credential format.
type Config struct {
	APIURL            string       `json:"api_url,omitempty"`
	OAuthConfigOrigin string       `json:"oauth_config_origin,omitempty"`
	OAuthConfig       *OAuthConfig `json:"oauth_config,omitempty"`
	// CredentialID remains stable across token rotation and changes for each new saved login.
	CredentialID     string               `json:"credential_id,omitempty"`
	Token            string               `json:"token"`
	RefreshToken     string               `json:"refresh_token,omitempty"`
	ExpiresAt        *time.Time           `json:"expires_at,omitempty"`
	CredentialSource Source               `json:"credential_source,omitempty"`
	CredentialType   types.CredentialType `json:"credential_type,omitempty"`
}

type projectConfig struct {
	APIURL string `json:"api_url,omitempty"`
}

func Path() (string, error) {
	dir, err := utils.ThunderDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cli_config.json"), nil
}

// Load returns the active credential. TNR_API_TOKEN takes precedence without
// modifying or exposing any saved credential.
func Load() (*Config, error) {
	if token := os.Getenv("TNR_API_TOKEN"); token != "" {
		config := &Config{
			APIURL:           DefaultAPIURL,
			Token:            token,
			CredentialSource: SourceEnvironment,
		}
		applyAPIURLOverrides(config)
		return config, nil
	}

	path, err := Path()
	if err != nil {
		return nil, err
	}
	return loadStoredEffective(path)
}

func loadStored(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func loadStoredEffective(path string) (*Config, error) {
	config, err := loadStored(path)
	if err != nil {
		return nil, err
	}
	if config.APIURL == "" {
		config.APIURL = DefaultAPIURL
	}
	applyAPIURLOverrides(config)
	return config, nil
}

func applyAPIURLOverrides(config *Config) {
	if project := loadProjectConfig(); project != nil && project.APIURL != "" {
		config.APIURL = project.APIURL
	}
	if apiURL := os.Getenv("TNR_API_URL"); apiURL != "" {
		config.APIURL = apiURL
	}
}

func loadProjectConfig() *projectConfig {
	data, err := os.ReadFile(".thunder.json")
	if err != nil {
		return nil
	}

	var config projectConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil
	}
	return &config
}

// Save atomically replaces the credential file while holding the same lock used by refresh and logout. A config without a CredentialID represents a new login.
func Save(ctx context.Context, config Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if config.CredentialID == "" {
		config.CredentialID, err = randomURLSafe(32)
		if err != nil {
			return fmt.Errorf("generate credential identity: %w", err)
		}
	}
	return withFileLock(ctx, path, func() error {
		return writeAtomic(path, config)
	})
}

// Delete removes the saved credential while holding the refresh lock.
func Delete(ctx context.Context) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return withFileLock(ctx, path, func() error {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
}

func writeAtomic(path string, config Config) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("secure credential directory: %w", err)
	}

	temp, err := os.CreateTemp(dir, ".cli-config-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tempPath, path); err != nil {
		return err
	}
	// The file itself is durable before the rename. Directory fsync is a
	// best-effort hardening step because some supported filesystems reject it.
	_ = syncDirectory(dir)
	return nil
}
