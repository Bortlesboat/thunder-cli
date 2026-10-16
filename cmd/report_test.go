package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportJSONSubmissionOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, code string
		success    bool
	}{
		{"confirmed", 200, `{"ok":true,"report_id":"report-123"}`, "", true},
		{"rejected", 400, `{"error":"invalid_support_ticket","message":"Rejected"}`, "invalid_support_ticket", false},
		{"plain rate limit", 429, `slow down`, "support_rate_limited", false},
		{"malformed success", 200, `{"ok":`, "support_submission_failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				assert.Equal(t, "/support/tickets", r.URL.Path)
				assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				var body api.ReportRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				assert.Equal(t, "cli", body.Source)
				assert.Equal(t, "feedback", body.ReportKind)
				assert.Equal(t, "product_feedback", body.Category)
				assert.Equal(t, "Selected evidence", body.Details)
				assert.NotEmpty(t, body.Context.CLIVersion)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result := runCLI(t, map[string]string{"TNR_API_TOKEN": "test-token", "TNR_API_URL": server.URL}, "report", "--subject", "Feedback", "--details", "Selected evidence", "--kind", "feedback", "--yes", "--json")
			var output map[string]any
			decodeSingleJSONDocument(t, result.stdout, &output)
			assert.Equal(t, tc.success, output["ok"])
			if tc.success {
				assert.Equal(t, 0, result.exitCode)
				assert.Equal(t, "report-123", output["report_id"])
			} else {
				assert.Equal(t, 1, result.exitCode)
				assert.Equal(t, tc.code, output["error"])
				assert.Equal(t, "Selected evidence", output["report"].(map[string]any)["details"])
			}
			assert.Empty(t, result.stderr)
			assert.Equal(t, int32(1), count.Load())
		})
	}
}

func TestReportDryRunAndInputGuardsSendNothing(t *testing.T) {
	file := filepath.Join(t.TempDir(), "report.txt")
	require.NoError(t, os.WriteFile(file, []byte("Evidence from file"), 0600))
	for _, tc := range []struct {
		name    string
		args    []string
		success bool
	}{
		{"dry run without auth", []string{"--subject", "Bug", "--details-file", file, "--dry-run"}, true},
		{"requires confirmation", []string{"--subject", "Bug", "--details", "Evidence"}, false},
		{"requires text", []string{"--yes"}, false},
		{"conflicting details", []string{"--subject", "Bug", "--details", "Evidence", "--details-file", file, "--dry-run"}, false},
		{"conflicting resources", []string{"--subject", "Bug", "--details", "Evidence", "--instance", "1", "--sandbox", "2", "--dry-run"}, false},
		{"invalid timestamp", []string{"--subject", "Bug", "--details", "Evidence", "--occurred-at", "yesterday", "--dry-run"}, false},
		{"oversized details", []string{"--subject", "Bug", "--details", strings.Repeat("x", 10001), "--dry-run"}, false},
		{"unicode subject", []string{"--subject", strings.Repeat("界", 200), "--details", "Evidence", "--dry-run"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runCLI(t, nil, append([]string{"report", "--json"}, tc.args...)...)
			var output map[string]any
			decodeSingleJSONDocument(t, result.stdout, &output)
			if tc.success {
				assert.Equal(t, 0, result.exitCode)
				assert.Equal(t, true, output["dry_run"])
				assert.Equal(t, false, output["submitted"])
			} else {
				assert.Equal(t, 1, result.exitCode)
			}
			assert.Empty(t, result.stderr)
		})
	}
}

func TestReportStdinDryRun(t *testing.T) {
	oldJSON := JSONOutput
	JSONOutput = true
	t.Cleanup(func() { JSONOutput = oldJSON })
	cmd := newReportCommand()
	cmd.SetIn(strings.NewReader("Evidence from stdin"))
	require.NoError(t, cmd.ParseFlags([]string{"--subject", "Bug", "--details-file", "-", "--dry-run"}))
	saved := os.Stdout
	read, write, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = saved })
	require.NoError(t, runReport(cmd, nil))
	require.NoError(t, write.Close())
	os.Stdout = saved
	output, err := io.ReadAll(read)
	require.NoError(t, err)
	require.NoError(t, read.Close())
	var preview reportPreview
	require.NoError(t, json.Unmarshal(output, &preview))
	assert.Equal(t, "Evidence from stdin", preview.Report.Details)
}

func TestReportSubmissionErrorRetainsInput(t *testing.T) {
	for _, cause := range []error{
		context.Canceled,
		errors.New("failed to load authentication token"),
		&api.APIError{Code: "support_submission_failed", Message: "Could not submit."},
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			err := &reportSubmissionError{Err: cause, Report: api.ReportRequest{Subject: "GPU issue", Details: "Selected evidence"}}
			require.ErrorIs(t, err, cause)
			assert.Contains(t, err.Error(), "Selected evidence")
			body, marshalErr := json.Marshal(err.JSONErrorOutput())
			require.NoError(t, marshalErr)
			var output struct {
				OK     bool              `json:"ok"`
				Error  string            `json:"error"`
				Report api.ReportRequest `json:"report"`
			}
			require.NoError(t, json.Unmarshal(body, &output))
			assert.False(t, output.OK)
			assert.Equal(t, "support_submission_failed", output.Error)
			assert.Equal(t, err.Report, output.Report)
		})
	}
}
