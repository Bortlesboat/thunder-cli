package credentials

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Thunder-Compute/thunder-cli/internal/clierr"
)

const oauthConfigPath = "/v1/auth/cli/oauth-config"

type OAuthConfig struct {
	AuthorizationURL string   `json:"authorization_url"`
	TokenURL         string   `json:"token_url"`
	RevocationURL    string   `json:"revocation_url"`
	ClientID         string   `json:"client_id"`
	Resource         string   `json:"resource"`
	Scopes           []string `json:"scopes"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (c OAuthConfig) Validate() error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "authorization URL", value: c.AuthorizationURL},
		{name: "token URL", value: c.TokenURL},
		{name: "revocation URL", value: c.RevocationURL},
		{name: "client ID", value: c.ClientID},
		{name: "resource", value: c.Resource},
	}
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("CLI OAuth config is missing %s", field.name)
		}
	}
	if len(c.Scopes) == 0 {
		return errors.New("CLI OAuth config is missing scopes")
	}
	configuredScopes := make(map[string]struct{}, len(c.Scopes))
	for _, scope := range c.Scopes {
		configuredScopes[scope] = struct{}{}
	}
	for _, required := range []string{"openid", "offline_access", "cli:access"} {
		if _, ok := configuredScopes[required]; !ok {
			return fmt.Errorf("CLI OAuth config is missing required %s scope", required)
		}
	}
	for _, endpoint := range []string{c.AuthorizationURL, c.TokenURL, c.RevocationURL} {
		if err := validateOAuthEndpoint(endpoint); err != nil {
			return err
		}
	}
	return nil
}

func validateOAuthEndpoint(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("invalid CLI OAuth endpoint %q", rawURL)
	}
	if parsed.Scheme == "https" {
		return nil
	}
	if parsed.Scheme == "http" {
		host := parsed.Hostname()
		if host == "localhost" || net.ParseIP(host).IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("CLI OAuth endpoint must use HTTPS: %s", rawURL)
}

type TokenResponse struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	Scope        string    `json:"scope"`
	ReceivedAt   time.Time `json:"-"`
}

func (r TokenResponse) Validate(requireRefresh bool) error {
	if r.AccessToken == "" {
		return errors.New("OAuth token response did not include an access token")
	}
	if requireRefresh && r.RefreshToken == "" {
		return errors.New("OAuth token response did not include a rotated refresh token")
	}
	if r.ExpiresIn <= 0 {
		return errors.New("OAuth token response included an invalid expiry")
	}
	if r.TokenType != "" && !strings.EqualFold(r.TokenType, "bearer") {
		return fmt.Errorf("OAuth token response used unsupported token type %q", r.TokenType)
	}
	return nil
}

type ProviderError struct {
	StatusCode   int
	Code         string
	ProviderCode string
	ErrorURI     string
	Description  string
}

func (e *ProviderError) Error() string {
	if code := firstNonEmpty(e.Code, e.ProviderCode); code != "" {
		return fmt.Sprintf("OAuth provider rejected the request (%s)", code)
	}
	return fmt.Sprintf("OAuth provider returned status %d", e.StatusCode)
}

func (e *ProviderError) InvalidGrant() bool {
	return invalidGrantCode(e.Code) || invalidGrantCode(e.ProviderCode) || invalidGrantCode(errorURIFragment(e.ErrorURI))
}

func errorURIFragment(rawURI string) string {
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return ""
	}
	return parsed.Fragment
}

func invalidGrantCode(code string) bool {
	switch code {
	case "invalid_grant",
		"invalid_refresh_token",
		"oauth_refresh_token_not_found",
		"connected_app_consent_revoked",
		"idp_refresh_token_expired",
		"idp_refresh_token_already_used",
		"idp_refresh_token_not_found":
		return true
	default:
		return false
	}
}

type oauthClient struct {
	httpClient *http.Client
}

func newOAuthClient(client *http.Client) oauthClient {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return oauthClient{httpClient: client}
}

func FetchOAuthConfig(ctx context.Context, apiURL string) (*OAuthConfig, error) {
	config, err := newOAuthClient(nil).FetchConfig(ctx, apiURL)
	return config, clierr.Mark(err)
}

func ExchangeAuthorizationCode(ctx context.Context, config OAuthConfig, code, redirectURI, verifier string) (*TokenResponse, error) {
	response, err := newOAuthClient(nil).ExchangeCode(ctx, config, code, redirectURI, verifier)
	return response, clierr.Mark(err)
}

func (c oauthClient) FetchConfig(ctx context.Context, apiURL string) (*OAuthConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiURL, "/")+oauthConfigPath, nil)
	if err != nil {
		return nil, fmt.Errorf("create CLI OAuth config request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	var config OAuthConfig
	if err := c.doJSON(req, &config); err != nil {
		return nil, fmt.Errorf("fetch CLI OAuth config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &config, nil
}

func (c oauthClient) ExchangeCode(ctx context.Context, config OAuthConfig, code, redirectURI, verifier string) (*TokenResponse, error) {
	form := url.Values{
		"client_id":     {config.ClientID},
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"resource":      {config.Resource},
	}
	return c.exchange(ctx, config.TokenURL, form, true)
}

func (c oauthClient) Refresh(ctx context.Context, config OAuthConfig, refreshToken string) (*TokenResponse, error) {
	form := url.Values{
		"client_id":     {config.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"resource":      {config.Resource},
	}
	return c.exchange(ctx, config.TokenURL, form, true)
}

func (c oauthClient) exchange(ctx context.Context, endpoint string, form url.Values, requireRefresh bool) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create OAuth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	var response TokenResponse
	if err := c.doJSON(req, &response); err != nil {
		return nil, err
	}
	response.ReceivedAt = time.Now().UTC()
	if err := response.Validate(requireRefresh); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c oauthClient) doJSON(req *http.Request, target any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("OAuth transport failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read OAuth response: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		var provider struct {
			Error            string `json:"error"`
			ErrorType        string `json:"error_type"`
			ErrorDescription string `json:"error_description"`
			ErrorMessage     string `json:"error_message"`
			ErrorURI         string `json:"error_uri"`
		}
		_ = json.Unmarshal(body, &provider)
		return &ProviderError{
			StatusCode:   resp.StatusCode,
			Code:         provider.Error,
			ProviderCode: provider.ErrorType,
			ErrorURI:     provider.ErrorURI,
			Description:  firstNonEmpty(provider.ErrorDescription, provider.ErrorMessage),
		}
	}
	if target == nil || len(body) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode OAuth response: %w", err)
	}
	return nil
}

func GenerateState() (string, error) {
	return randomURLSafe(32)
}

func GenerateCodeVerifier() (string, error) {
	return randomURLSafe(32)
}

func randomURLSafe(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func CodeChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func AuthorizationURL(config OAuthConfig, redirectURI, state, challenge string) (string, error) {
	authorizeURL, err := url.Parse(config.AuthorizationURL)
	if err != nil {
		return "", err
	}
	params := authorizeURL.Query()
	params.Set("client_id", config.ClientID)
	params.Set("resource", config.Resource)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(config.Scopes, " "))
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	authorizeURL.RawQuery = params.Encode()
	return authorizeURL.String(), nil
}
