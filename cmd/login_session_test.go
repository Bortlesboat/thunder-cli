package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLoginToken(t *testing.T) {
	state := "current+state="
	callback := "http://127.0.0.1:43123/callback?state=" + url.QueryEscape(state)
	for _, tc := range []struct {
		name, input, token, errorText string
	}{
		{"raw token", "  raw-token\n", "raw-token", ""},
		{"encoded callback", callback + "&token=a%2Bb%2Fc%3D&theme=light", "a+b/c=", ""},
		{"long callback", callback + "&token=" + strings.Repeat("a", 600), strings.Repeat("a", 600), ""},
		{"empty", " ", "", "paste a token"},
		{"missing token", callback, "", "no token"},
		{"duplicate token", callback + "&token=a&token=b", "", "no token"},
		{"stale state", "http://127.0.0.1:43123/callback?state=old&token=secret", "", "different login"},
		{"missing state", "http://127.0.0.1:43123/callback?token=secret", "", "different login"},
		{"duplicate state", callback + "&state=other&token=secret", "", "different login"},
		{"login page", "https://console.thundercompute.com/login/app?state=current", "", "localhost callback"},
		{"untrusted hostname", "http://127.0.0.1.example/callback?token=secret", "", "localhost callback"},
		{"malformed encoding", callback + "&token=%zz", "", "malformed"},
		{"auth failure", callback + "&error=failed", "", "did not complete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			credentials, err := parseLoginToken(tc.input, state)
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
				assert.NotContains(t, err.Error(), "secret")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.token, credentials.Token)
		})
	}
}

func TestCallbackRetriesAndAcknowledgesOnlySavedCredentials(t *testing.T) {
	var validations atomic.Int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		validations.Add(1)
		assert.Equal(t, "/v1/auth/validate", r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"email":"member@example.com","org_name":"Example"}`)
	}))
	defer apiServer.Close()
	t.Setenv("TNR_API_URL", apiServer.URL)
	t.Setenv("TNR_HOME", t.TempDir())
	t.Setenv("TNR_API_TOKEN", "")
	session := &loginSession{ctx: context.Background()}
	port, successes, _, cleanup, err := startCallbackServerWithContext(session.ctx, "current-state", session.authenticate)
	require.NoError(t, err)
	defer cleanup()
	httpClient := &http.Client{Timeout: time.Second}

	request := func(query string, wantStatus int) string {
		t.Helper()
		response, err := httpClient.Get(fmt.Sprintf("http://127.0.0.1:%d/callback?%s", port, query))
		require.NoError(t, err)
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		assert.Equal(t, wantStatus, response.StatusCode)
		assert.Equal(t, "no-store", response.Header.Get("Cache-Control"))
		return string(body)
	}
	for range 3 {
		body := request("state=stale&token=wrong&ack=1", http.StatusBadRequest)
		assert.NotContains(t, body, "postMessage")
	}
	assert.Zero(t, validations.Load())
	body := request("state=current-state&token=bad-token&ack=1", http.StatusUnauthorized)
	assert.NotContains(t, body, "postMessage")
	_, err = LoadConfig()
	require.Error(t, err)

	body = request("state=current-state&token=good-token&ack=1", http.StatusOK)
	assert.Contains(t, body, `state: "current-state"`)
	assert.Contains(t, body, "thunder-auth-complete")
	assert.Contains(t, body, "https://console.thundercompute.com")
	config, err := LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "good-token", config.Token)
	assert.Equal(t, "good-token", (<-successes).Token)

	// Repeated browser and manual delivery must not validate or save again.
	request("state=current-state&token=good-token&ack=1", http.StatusOK)
	require.NoError(t, session.authenticate(AuthResponse{Token: "good-token"}))
	assert.EqualValues(t, 2, validations.Load())
	body = request("state=current-state&token=good-token", http.StatusOK)
	assert.NotContains(t, body, "postMessage")
}

func TestConcurrentLoginCompletionSavesOnce(t *testing.T) {
	var validations atomic.Int32
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		validations.Add(1)
		started <- struct{}{}
		<-release
		_, _ = io.WriteString(w, `{"email":"member@example.com"}`)
	}))
	defer apiServer.Close()
	t.Setenv("TNR_API_URL", apiServer.URL)
	t.Setenv("TNR_HOME", t.TempDir())
	session := &loginSession{ctx: context.Background()}
	results := make(chan error, 2)
	go func() { results <- session.authenticate(AuthResponse{Token: "same-token"}) }()
	<-started
	go func() { results <- session.authenticate(AuthResponse{Token: "same-token"}) }()
	close(release)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	assert.EqualValues(t, 1, validations.Load())
}

func TestCancellationDuringLoginValidationDoesNotSave(t *testing.T) {
	started := make(chan struct{})
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer apiServer.Close()
	t.Setenv("TNR_API_URL", apiServer.URL)
	t.Setenv("TNR_HOME", t.TempDir())
	t.Setenv("TNR_API_TOKEN", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := &loginSession{ctx: ctx}
	result := make(chan error, 1)
	go func() { result <- session.authenticate(AuthResponse{Token: "token"}) }()
	<-started
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	_, err := LoadConfig()
	require.Error(t, err)
}

func TestCancelledLoginDoesNotSaveCredentials(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	t.Setenv("TNR_HOME", t.TempDir())
	t.Setenv("TNR_API_TOKEN", "")
	session := &loginSession{ctx: ctx}
	require.ErrorIs(t, session.authenticate(AuthResponse{Token: "token"}), context.Canceled)
	_, err := LoadConfig()
	require.Error(t, err)
}

func TestSSHSelectsManualLoginWithExplicitOverride(t *testing.T) {
	flag := loginCmd.Flags().Lookup("manual")
	originalChanged, originalManual := flag.Changed, loginManual
	t.Cleanup(func() { flag.Changed, loginManual = originalChanged, originalManual })
	for _, variable := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Run(variable, func(t *testing.T) {
			t.Setenv(variable, "ssh-session")
			flag.Changed, loginManual = false, false
			assert.True(t, useManualLogin())
			flag.Changed = true
			assert.False(t, useManualLogin())
			loginManual = true
			assert.True(t, useManualLogin())
		})
	}
}
