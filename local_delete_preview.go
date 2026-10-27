package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Thunder-Compute/thunder-cli/internal/version"
)

func init() {
	if os.Getenv("TNR_MOCK_DELETE") != "1" {
		return
	}
	if len(os.Args) < 2 || (os.Args[1] != "delete" &&
		!(len(os.Args) > 2 && os.Args[1] == "snapshot" && os.Args[2] == "delete")) {
		fmt.Fprintln(os.Stderr, "TNR_MOCK_DELETE supports only delete and snapshot delete.")
		os.Exit(2)
	}
	previewDir, err := os.MkdirTemp("", "tnr-delete-preview-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for key, value := range map[string]string{
		"TNR_HOME":          previewDir,
		"TNR_API_TOKEN":     "local-preview-token",
		"TNR_API_URL":       "https://delete-preview.invalid",
		"TNR_NO_SELFUPDATE": "1",
	} {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	version.SentryDSN = ""
	http.DefaultTransport = deletePreviewTransport{}
	fmt.Fprintln(os.Stderr, "Mock deletion preview: sample data only; deletion is disabled.")
}

type deletePreviewTransport struct{}

func (deletePreviewTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	status := http.StatusOK
	var body string
	switch req.Method + " " + req.URL.Path {
	case "GET /v1/instances/list":
		body = `{
			"0":{"id":"0","uuid":"preview-training","name":"training-demo","status":"RUNNING","ip":"192.0.2.10","storage":100,"cpuCores":"8","gpuType":"l40","numGpus":"1","memory":"48","template":"base"},
			"1":{"id":"1","uuid":"preview-notebook","name":"notebook-demo","status":"RUNNING","ip":"192.0.2.11","storage":200,"cpuCores":"6","gpuType":"a6000","numGpus":"1","memory":"48","template":"base"}
		}`
	case "GET /v1/snapshots/list":
		body = `[
			{"id":"preview-snapshot-1","name":"demo-snapshot","minimumDiskSizeGb":100,"status":"READY","createdAt":1789516800},
			{"id":"preview-snapshot-2","name":"training-backup","minimumDiskSizeGb":200,"status":"READY","createdAt":1789603200}
		]`
	default:
		// Reject mutations before the command can perform local SSH cleanup.
		status = http.StatusForbidden
		body = `{"message":"Mock preview: this request is disabled; nothing was changed."}`
	}
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}, nil
}
