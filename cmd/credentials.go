package cmd

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/Thunder-Compute/thunder-cli/internal/clierr"
	"github.com/Thunder-Compute/thunder-cli/internal/credentials"
	clitypes "github.com/Thunder-Compute/thunder-cli/pkg/types"
)

type Config = credentials.Config
type CredentialSource = credentials.Source

const (
	CredentialSourceBrowser       = credentials.SourceBrowser
	CredentialSourceProvidedToken = credentials.SourceProvidedToken
	CredentialSourceEnvironment   = credentials.SourceEnvironment
	DefaultAPIURL                 = credentials.DefaultAPIURL
)

type AuthResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

func getAPIURL() string {
	if envURL := os.Getenv("TNR_API_URL"); envURL != "" {
		return envURL
	}
	return DefaultAPIURL
}

func LoadConfig() (*Config, error) {
	return credentials.Load()
}

func saveConfig(authResp AuthResponse, source CredentialSource, result *api.ValidateTokenResult) error {
	config := Config{
		Token:            authResp.Token,
		RefreshToken:     authResp.RefreshToken,
		CredentialSource: source,
	}
	if apiURL := getAPIURL(); apiURL != DefaultAPIURL {
		config.APIURL = apiURL
	}
	if result != nil {
		config.CredentialType = result.CredentialType
		config.ExpiresAt = result.ExpiresAt
	}
	return credentials.Save(context.Background(), config)
}

func newAuthenticatedClient(config *Config) (*api.Client, error) {
	apiURL, err := authenticatedAPIURL(config)
	if err != nil {
		return nil, err
	}
	var client *api.Client
	if config.CredentialSource == CredentialSourceBrowser && config.CredentialType == clitypes.CredentialTypeCLIOAuth {
		manager, err := credentials.NewManager(config)
		if err != nil {
			return nil, err
		}
		client = api.NewClientWithTokenSource(manager, apiURL)
	} else {
		client = api.NewClient(config.Token, apiURL)
	}
	message := "your saved credentials are no longer valid. Run 'tnr login' to sign in again"
	if config.CredentialSource == CredentialSourceEnvironment {
		message = "TNR_API_TOKEN was rejected. Replace it, or unset it and run 'tnr login'"
	} else if config.CredentialType == clitypes.CredentialTypeAPIToken {
		message = "your API token was rejected. Replace it with 'tnr login --token <token>', or run 'tnr login' for browser sign-in"
	}
	client.SetAuthenticationErrorMessage(message)
	return client, nil
}

func authenticatedAPIURL(config *Config) (string, error) {
	if config.CredentialSource != CredentialSourceBrowser || config.CredentialType != clitypes.CredentialTypeCLIOAuth {
		return config.APIURL, nil
	}
	trustedOrigin, err := apiOrigin(config.OAuthConfigOrigin)
	if err != nil {
		return "", clierr.Mark(fmt.Errorf("%w: saved CLI OAuth login has a missing or invalid trusted API origin", credentials.ErrReauthenticationRequired))
	}
	effectiveOrigin, err := apiOrigin(config.APIURL)
	if err != nil || effectiveOrigin != trustedOrigin {
		return "", clierr.Mark(fmt.Errorf("%w: configured API origin does not match the saved CLI OAuth login; remove the API URL override or log in for the intended API", credentials.ErrReauthenticationRequired))
	}
	return strings.TrimRight(config.OAuthConfigOrigin, "/"), nil
}

func apiOrigin(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", fmt.Errorf("invalid API URL")
	}
	port := parsed.Port()
	if port == "" {
		port = "443"
		if parsed.Scheme == "http" {
			port = "80"
		}
	}
	return parsed.Scheme + "://" + net.JoinHostPort(strings.ToLower(parsed.Hostname()), port), nil
}
