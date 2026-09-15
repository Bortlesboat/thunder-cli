package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Thunder-Compute/thunder-cli/internal/credentials"
	clitypes "github.com/Thunder-Compute/thunder-cli/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testOAuthCredential(apiURL string) Config {
	expiresAt := time.Now().Add(time.Hour)
	return Config{
		APIURL:            apiURL,
		OAuthConfigOrigin: apiURL,
		OAuthConfig: &credentials.OAuthConfig{
			AuthorizationURL: apiURL + "/authorize",
			TokenURL:         apiURL + "/token",
			RevocationURL:    apiURL + "/revoke",
			ClientID:         "test-cli",
			Resource:         "test-api",
			Scopes:           []string{"openid", "offline_access", "cli:access"},
		},
		CredentialID:     "saved-login",
		Token:            "saved-access-token",
		RefreshToken:     "saved-refresh-token",
		ExpiresAt:        &expiresAt,
		CredentialSource: CredentialSourceBrowser,
		CredentialType:   clitypes.CredentialTypeCLIOAuth,
	}
}

func writeTestCredential(t *testing.T, config Config) (string, []byte) {
	t.Helper()
	path, err := credentials.Path()
	require.NoError(t, err)
	data, err := json.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path, data
}

func TestAuthenticatedClientRefreshesBeforeReportingReauthentication(t *testing.T) {
	for _, tt := range []struct {
		name           string
		providerStatus int
		retryStatus    int
	}{
		{"refresh restores access", http.StatusOK, http.StatusOK},
		{"refresh grant is revoked", http.StatusBadRequest, 0},
		{"provider is unavailable", http.StatusServiceUnavailable, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TNR_HOME", t.TempDir())
			var apiRequests, refreshRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/instances/list":
					if apiRequests.Add(1) == 1 {
						assert.Equal(t, "Bearer saved-access-token", r.Header.Get("Authorization"))
						w.WriteHeader(http.StatusUnauthorized)
					} else {
						assert.Equal(t, "Bearer refreshed-access-token", r.Header.Get("Authorization"))
						w.WriteHeader(tt.retryStatus)
					}
					_, _ = io.WriteString(w, `{}`)
				case "/token":
					refreshRequests.Add(1)
					w.WriteHeader(tt.providerStatus)
					switch tt.providerStatus {
					case http.StatusOK:
						_, _ = io.WriteString(w, `{"access_token":"refreshed-access-token","refresh_token":"refreshed-refresh-token","expires_in":3600,"token_type":"Bearer"}`)
					case http.StatusBadRequest:
						_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
					default:
						_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
					}
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			config := testOAuthCredential(server.URL)
			path, before := writeTestCredential(t, config)
			client, err := newAuthenticatedClient(&config)
			require.NoError(t, err)
			_, err = client.ListInstances()
			require.Equal(t, int32(1), refreshRequests.Load())
			if tt.providerStatus == http.StatusOK {
				require.Equal(t, int32(2), apiRequests.Load())
				require.NoError(t, err)
				return
			}
			require.Equal(t, int32(1), apiRequests.Load())
			var providerErr *credentials.ProviderError
			require.ErrorAs(t, err, &providerErr)
			assert.Equal(t, tt.providerStatus, providerErr.StatusCode)
			assert.Equal(t, tt.providerStatus == http.StatusBadRequest, errors.Is(err, credentials.ErrReauthenticationRequired))
			after, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, before, after)
		})
	}
}

func TestLogoutRequiresConfirmedRevocationBeforeRemovingCredentials(t *testing.T) {
	tests := []struct {
		name          string
		apiStatus     int
		expired       bool
		loseResponse  bool
		refreshOK     bool
		wantRevokes   int32
		wantRefreshes int32
		wantError     bool
	}{
		{name: "confirmed revocation", apiStatus: http.StatusOK, wantRevokes: 1},
		{name: "expired access token and rejected refresh", expired: true, wantRefreshes: 1, wantError: true},
		{name: "rejected access token and refresh", apiStatus: http.StatusUnauthorized, wantRevokes: 1, wantRefreshes: 1, wantError: true},
		{name: "refreshed access token rejected", apiStatus: http.StatusUnauthorized, refreshOK: true, wantRevokes: 2, wantRefreshes: 1, wantError: true},
		{name: "temporary API failure", apiStatus: http.StatusServiceUnavailable, wantRevokes: 1, wantError: true},
		{name: "revocation response lost", loseResponse: true, wantRevokes: 1, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TNR_HOME", dir)
			t.Chdir(t.TempDir())
			var revokeRequests, refreshRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/auth/cli/grant":
					request := revokeRequests.Add(1)
					assert.Equal(t, http.MethodDelete, r.Method)
					wantToken := "saved-access-token"
					if tt.refreshOK && request > 1 {
						wantToken = "refreshed-access-token"
					}
					assert.Equal(t, "Bearer "+wantToken, r.Header.Get("Authorization"))
					if tt.loseResponse {
						conn, _, err := w.(http.Hijacker).Hijack()
						if assert.NoError(t, err) {
							assert.NoError(t, conn.Close())
						}
						return
					}
					w.WriteHeader(tt.apiStatus)
				case "/token":
					refreshRequests.Add(1)
					assert.Equal(t, http.MethodPost, r.Method)
					if tt.refreshOK {
						_, _ = io.WriteString(w, `{"access_token":"refreshed-access-token","refresh_token":"refreshed-refresh-token","expires_in":3600,"token_type":"Bearer"}`)
						return
					}
					w.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)
			config := testOAuthCredential(server.URL)
			if tt.expired {
				expired := time.Now().Add(-time.Minute)
				config.ExpiresAt = &expired
			}
			path, before := writeTestCredential(t, config)
			args := []string{"logout", "--revoke", "--json"}
			result := runCLI(t, map[string]string{"TNR_HOME": dir}, args...)
			assert.Equal(t, tt.wantRevokes, revokeRequests.Load())
			assert.Equal(t, tt.wantRefreshes, refreshRequests.Load())
			if !tt.wantError {
				require.Equal(t, 0, result.exitCode, "stdout: %s\nstderr: %s", result.stdout, result.stderr)
				_, err := os.Stat(path)
				require.ErrorIs(t, err, os.ErrNotExist)
				return
			}
			require.Equal(t, 1, result.exitCode, "stdout: %s\nstderr: %s", result.stdout, result.stderr)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			if tt.refreshOK {
				var saved Config
				require.NoError(t, json.Unmarshal(after, &saved))
				assert.Equal(t, config.CredentialID, saved.CredentialID)
				assert.Equal(t, "refreshed-access-token", saved.Token)
				assert.Equal(t, "refreshed-refresh-token", saved.RefreshToken)
			} else {
				assert.Equal(t, before, after)
			}
		})
	}
}

func TestSubscriptionCheckUsesTrustedOriginWithoutRefreshing(t *testing.T) {
	for _, name := range []string{
		"trusted origin", "same origin path override", "expired token", "unauthorized token",
		"missing origin", "invalid origin", "environment override", "project override",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TNR_HOME", t.TempDir())
			t.Setenv("TNR_API_TOKEN", "")
			t.Setenv("TNR_API_URL", "")
			t.Chdir(t.TempDir())

			var trustedRequests, overrideRequests, refreshRequests atomic.Int32
			trusted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/user-data" {
					refreshRequests.Add(1)
					http.NotFound(w, r)
					return
				}
				trustedRequests.Add(1)
				assert.Equal(t, "Bearer saved-access-token", r.Header.Get("Authorization"))
				if name == "unauthorized token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				_, _ = io.WriteString(w, `{"data":{"subscriptionStatus":"past_due"}}`)
			}))
			t.Cleanup(trusted.Close)
			override := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				overrideRequests.Add(1)
				_, _ = io.WriteString(w, `{"data":{"subscriptionStatus":"past_due"}}`)
			}))
			t.Cleanup(override.Close)

			config := testOAuthCredential(trusted.URL)
			wantTrusted := int32(0)
			switch name {
			case "trusted origin", "unauthorized token":
				wantTrusted = 1
			case "same origin path override":
				t.Setenv("TNR_API_URL", trusted.URL+"/untrusted-path")
				wantTrusted = 1
			case "expired token":
				expired := time.Now().Add(-time.Minute)
				config.ExpiresAt = &expired
				wantTrusted = 1
			case "missing origin":
				config.OAuthConfigOrigin = ""
			case "invalid origin":
				config.OAuthConfigOrigin = "://invalid"
			case "environment override":
				t.Setenv("TNR_API_URL", override.URL)
			case "project override":
				data, err := json.Marshal(map[string]string{"api_url": override.URL})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(".thunder.json", data, 0o600))
			}
			path, before := writeTestCredential(t, config)
			loaded, err := LoadConfig()
			require.NoError(t, err)

			refreshSubscriptionStatus(context.Background(), loaded)

			assert.Equal(t, wantTrusted, trustedRequests.Load())
			assert.Zero(t, overrideRequests.Load())
			assert.Zero(t, refreshRequests.Load(), "subscription checks must not rotate credentials")
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}

func TestLoginReplacesRecoverableCredentials(t *testing.T) {
	tests := []struct {
		name             string
		apiStatus        int
		providerStatus   int
		environmentToken string
		wantError        bool
	}{
		{name: "revoked refresh token", apiStatus: http.StatusUnauthorized, providerStatus: http.StatusBadRequest},
		{name: "temporary API error", apiStatus: http.StatusServiceUnavailable, wantError: true},
		{name: "temporary provider error", apiStatus: http.StatusUnauthorized, providerStatus: http.StatusServiceUnavailable, wantError: true},
		{name: "invalid environment token", apiStatus: http.StatusUnauthorized, environmentToken: "invalid-environment-token", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("TNR_HOME", dir)
			t.Chdir(t.TempDir())
			var replacementRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/auth/validate":
					if r.Header.Get("Authorization") == "Bearer replacement-token" {
						replacementRequests.Add(1)
						_, _ = io.WriteString(w, `{"credential_type":"api_token"}`)
						return
					}
					status := tt.apiStatus
					if status == 0 {
						status = http.StatusUnauthorized
					}
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"message":"temporarily unavailable"}`)
				case "/token":
					status := tt.providerStatus
					if status == 0 {
						status = http.StatusBadRequest
					}
					w.WriteHeader(status)
					if status == http.StatusBadRequest {
						_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
					} else {
						_, _ = io.WriteString(w, `{"error":"temporarily_unavailable"}`)
					}
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			config := testOAuthCredential(server.URL)
			path, before := writeTestCredential(t, config)
			args := []string{"login", "--token", "replacement-token", "--json"}
			result := runCLI(t, map[string]string{
				"TNR_HOME":      dir,
				"TNR_API_URL":   server.URL,
				"TNR_API_TOKEN": tt.environmentToken,
			}, args...)
			after, err := os.ReadFile(path)
			require.NoError(t, err)

			if tt.wantError {
				assert.Equal(t, 1, result.exitCode)
				assert.Zero(t, replacementRequests.Load())
				assert.Equal(t, before, after)
				return
			}
			require.Equal(t, 0, result.exitCode, "stdout: %s\nstderr: %s", result.stdout, result.stderr)
			assert.Equal(t, int32(1), replacementRequests.Load())
			var saved Config
			require.NoError(t, json.Unmarshal(after, &saved))
			assert.Equal(t, "replacement-token", saved.Token)
			assert.Equal(t, CredentialSourceProvidedToken, saved.CredentialSource)
			assert.Equal(t, clitypes.CredentialTypeAPIToken, saved.CredentialType)
			assert.NotEmpty(t, saved.CredentialID)
			assert.NotEqual(t, config.CredentialID, saved.CredentialID)
			assert.Empty(t, saved.RefreshToken)
			assert.Nil(t, saved.OAuthConfig)
		})
	}
}
