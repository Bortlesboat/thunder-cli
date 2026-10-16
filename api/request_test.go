package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type reportRefreshTokenSource struct{ rejectedTokens []string }

func (s *reportRefreshTokenSource) Token(context.Context) (string, error) {
	return "expired-token", nil
}

func (s *reportRefreshTokenSource) Refresh(_ context.Context, rejectedToken string) (string, error) {
	s.rejectedTokens = append(s.rejectedTokens, rejectedToken)
	return "refreshed-token", nil
}

func TestSubmitReportRefreshesOnlyAfterDefiniteUnauthorizedResponse(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		firstStatus, nextStatus int
		loseResponse            bool
		requests                int32
		refreshes               int
		errorCode               string
	}{
		{name: "401 followed by receipt", firstStatus: 401, nextStatus: 200, requests: 2, refreshes: 1},
		{name: "second 401 stops", firstStatus: 401, nextStatus: 401, requests: 2, refreshes: 1, errorCode: "authorization_failed"},
		{name: "upstream failure stops", firstStatus: 502, requests: 1, errorCode: "support_submission_failed"},
		{name: "lost receipt stops", loseResponse: true, requests: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			bodies := make(chan string, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := requests.Add(1)
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				bodies <- string(body)
				token, status := "expired-token", tc.firstStatus
				if attempt > 1 {
					token, status = "refreshed-token", tc.nextStatus
				}
				assert.Equal(t, "Bearer "+token, r.Header.Get("Authorization"))
				if tc.loseResponse {
					conn, _, err := w.(http.Hijacker).Hijack()
					if assert.NoError(t, err) {
						assert.NoError(t, conn.Close())
					}
					return
				}
				w.WriteHeader(status)
				if status == http.StatusOK {
					_, _ = io.WriteString(w, `{"ok":true,"report_id":"report-123"}`)
				}
			}))
			defer server.Close()
			tokens := &reportRefreshTokenSource{}
			client := NewClientWithTokenSource(tokens, server.URL)
			result, err := client.SubmitReport(t.Context(), ReportRequest{Subject: "Synthetic report", Details: "Synthetic evidence", Source: "cli"})
			switch {
			case tc.loseResponse:
				require.ErrorIs(t, err, ErrTransport)
			case tc.errorCode == "":
				require.NoError(t, err)
				assert.Equal(t, "report-123", result.ReportID)
			default:
				var apiErr *APIError
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tc.errorCode, apiErr.Code)
			}
			assert.Equal(t, tc.requests, requests.Load())
			if tc.refreshes == 0 {
				assert.Empty(t, tokens.rejectedTokens)
			} else {
				assert.Equal(t, []string{"expired-token"}, tokens.rejectedTokens)
			}
			require.Equal(t, int(tc.requests), len(bodies))
			firstBody := <-bodies
			if tc.requests == 2 {
				assert.Equal(t, firstBody, <-bodies)
			}
		})
	}
}

type requestTestTransport func(*http.Request) (*http.Response, error)

func (f requestTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type trackedResponseBody struct {
	io.Reader
	closed bool
}

func (b *trackedResponseBody) Close() error {
	b.closed = true
	return nil
}

func TestSubmitReportRejectsOversizedReceipt(t *testing.T) {
	body := &trackedResponseBody{Reader: strings.NewReader(strings.Repeat("x", maxReportResponseBytes+1))}
	client := NewClient("token", "https://synthetic.invalid")
	client.httpClient.Transport = requestTestTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
	})
	_, err := client.SubmitReport(t.Context(), ReportRequest{})
	require.Error(t, err)
	assert.True(t, body.closed)
}
