package credentials

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Thunder-Compute/thunder-cli/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refreshTransport func(*http.Request) (*http.Response, error)

func (f refreshTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testRefreshManager(t *testing.T, transport refreshTransport) *Manager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cli_config.json")
	expiresAt := time.Now().Add(-time.Hour)
	config := Config{
		CredentialID:     "saved-login",
		CredentialSource: SourceBrowser,
		CredentialType:   types.CredentialTypeCLIOAuth,
		Token:            "old-access",
		RefreshToken:     "old-refresh",
		ExpiresAt:        &expiresAt,
		OAuthConfig: &OAuthConfig{
			AuthorizationURL: "https://auth.example/authorize",
			TokenURL:         "https://auth.example/token",
			RevocationURL:    "https://auth.example/revoke",
			ClientID:         "test-cli",
			Resource:         "test-api",
			Scopes:           []string{"openid", "offline_access", "cli:access"},
		},
	}
	require.NoError(t, writeAtomic(path, config))
	return newManager(path, &http.Client{Transport: transport}, config.CredentialID)
}

func TestRefreshPersistsRotationBeforeReturningCancellation(t *testing.T) {
	for _, proactive := range []bool{true, false} {
		for _, deadline := range []bool{true, false} {
			name := "unauthorized"
			if proactive {
				name = "expired"
			}
			if deadline {
				name += "/deadline"
			} else {
				name += "/cancelled"
			}
			t.Run(name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					calls := 0
					manager := testRefreshManager(t, func(r *http.Request) (*http.Response, error) {
						calls++
						require.NoError(t, r.ParseForm())
						assert.Equal(t, "old-refresh", r.Form.Get("refresh_token"))
						if deadline {
							time.Sleep(6 * time.Second)
						} else {
							cancel()
						}
						require.Error(t, ctx.Err())
						if err := r.Context().Err(); err != nil {
							return nil, err
						}
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600}`)),
						}, nil
					})

					var token string
					var err error
					if proactive {
						token, err = manager.Token(ctx)
					} else {
						token, err = manager.Refresh(ctx, "old-access")
					}
					require.ErrorIs(t, err, ctx.Err())
					assert.Empty(t, token)
					stored, err := loadStored(manager.path)
					require.NoError(t, err)
					assert.Equal(t, "new-access", stored.Token)
					assert.Equal(t, "new-refresh", stored.RefreshToken)
					assert.Equal(t, "saved-login", stored.CredentialID)
					require.NotNil(t, stored.ExpiresAt)
					assert.True(t, stored.ExpiresAt.After(time.Now()))
					token, err = manager.Token(context.Background())
					require.NoError(t, err)
					assert.Equal(t, "new-access", token)
					assert.Equal(t, 1, calls)
				})
			})
		}
	}
}

func TestRefreshDoesNotStartAfterCancellation(t *testing.T) {
	manager := testRefreshManager(t, func(r *http.Request) (*http.Response, error) {
		t.Error("refresh started after caller cancellation")
		return nil, context.Canceled
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	token, err := manager.Token(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, token)
	stored, err := loadStored(manager.path)
	require.NoError(t, err)
	assert.Equal(t, "old-refresh", stored.RefreshToken)
}

func TestRefreshHasIndependentDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		manager := testRefreshManager(t, func(r *http.Request) (*http.Response, error) {
			if _, ok := r.Context().Deadline(); !ok {
				t.Fatal("refresh must have a bounded deadline")
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		})
		token, err := manager.Token(context.Background())
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Empty(t, token)
		stored, err := loadStored(manager.path)
		require.NoError(t, err)
		assert.Equal(t, "old-refresh", stored.RefreshToken)
	})
}
