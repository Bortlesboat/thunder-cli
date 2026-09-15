package cmd

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/Thunder-Compute/thunder-cli/internal/credentials"
	"github.com/Thunder-Compute/thunder-cli/tui"
)

type loginSession struct {
	mu            sync.Mutex
	ctx           context.Context
	completionKey string
	result        *api.ValidateTokenResult
}

func (s *loginSession) authenticate(credential AuthResponse) error {
	return s.complete("token:"+credential.Token, func(ctx context.Context) (*api.ValidateTokenResult, error) {
		return completeInteractiveLogin(ctx, getAPIURL(), credentials.OAuthConfig{}, tui.LoginCompletion{
			Token: credential.Token, Source: tui.LoginCredentialSourceManual,
		}, nil)
	})
}

func (s *loginSession) authenticateBrowser(config credentials.OAuthConfig, code, redirectURI, verifier string) error {
	return s.complete("code:"+code, func(ctx context.Context) (*api.ValidateTokenResult, error) {
		response, err := credentials.ExchangeAuthorizationCode(ctx, config, code, redirectURI, verifier)
		if err != nil {
			return nil, fmt.Errorf("authorization code exchange failed: %w", err)
		}
		tokens := make(chan *credentials.TokenResponse, 1)
		tokens <- response
		return completeInteractiveLogin(ctx, getAPIURL(), config, tui.LoginCompletion{
			Token: response.AccessToken, Source: tui.LoginCredentialSourceBrowser,
		}, tokens)
	})
}

func (s *loginSession) complete(key string, authenticate func(context.Context) (*api.ValidateTokenResult, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if s.result != nil {
		if s.completionKey == key {
			return nil
		}
		return fmt.Errorf("this login has already completed")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	result, err := authenticate(ctx)
	if err != nil {
		return err
	}
	s.completionKey, s.result = key, result
	return nil
}

func parseLoginToken(input, expectedState string) (AuthResponse, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return AuthResponse{}, fmt.Errorf("paste a token or the complete callback URL")
	}
	if !strings.Contains(input, "://") && !strings.HasPrefix(input, "http:") && !strings.HasPrefix(input, "https:") {
		return AuthResponse{Token: input}, nil
	}

	callback, err := url.Parse(input)
	if err != nil || callback.Scheme != "http" || callback.User != nil || callback.Path != "/callback" || callback.Fragment != "" {
		return AuthResponse{}, fmt.Errorf("paste the token or the complete localhost callback URL from this login")
	}
	switch callback.Hostname() {
	case "127.0.0.1", "localhost", "::1":
	default:
		return AuthResponse{}, fmt.Errorf("paste the token or the complete localhost callback URL from this login")
	}
	params, err := url.ParseQuery(callback.RawQuery)
	if err != nil {
		return AuthResponse{}, fmt.Errorf("the callback URL is incomplete or malformed; copy it again")
	}
	if len(params["state"]) != 1 || expectedState == "" || subtle.ConstantTimeCompare([]byte(params.Get("state")), []byte(expectedState)) != 1 {
		return AuthResponse{}, fmt.Errorf("this callback URL belongs to a different login; open the link from the current login and try again")
	}
	if params.Get("error") != "" {
		return AuthResponse{}, fmt.Errorf("browser authentication did not complete; open the login link and try again")
	}
	if len(params["token"]) != 1 || strings.TrimSpace(params.Get("token")) == "" {
		return AuthResponse{}, fmt.Errorf("the callback URL has no token; copy the complete URL or the token shown in your browser")
	}
	return AuthResponse{Token: params.Get("token"), RefreshToken: params.Get("refresh_token")}, nil
}
