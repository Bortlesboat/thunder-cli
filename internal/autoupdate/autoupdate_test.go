package autoupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestVerifyArchiveChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	archivePath := filepath.Join(tmpDir, "tnr_9.9.9_linux_amd64.tar.gz")
	contents := []byte("trusted archive contents")
	if err := os.WriteFile(archivePath, contents, 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	hash := sha256.Sum256(contents)
	validChecksum := hex.EncodeToString(hash[:])

	var checksumHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checksumHits.Add(1)
		switch r.URL.Path {
		case "/valid":
			fmt.Fprintf(w, "%s  %s\n", validChecksum, filepath.Base(archivePath))
		case "/malformed":
			fmt.Fprintf(w, "not-a-sha256  %s\n", filepath.Base(archivePath))
		default:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	tests := []struct {
		name             string
		expectedChecksum string
		checksumURL      string
		wantError        string
	}{
		{
			name:             "valid expected checksum",
			expectedChecksum: validChecksum,
			checksumURL:      srv.URL + "/unavailable",
		},
		{
			name:             "mismatched expected checksum",
			expectedChecksum: strings.Repeat("0", 64),
			wantError:        "checksum mismatch",
		},
		{
			name:             "malformed expected checksum",
			expectedChecksum: "not-a-sha256",
			wantError:        "invalid SHA-256 checksum",
		},
		{
			name:      "missing checksum",
			wantError: "no checksum or checksum URL provided",
		},
		{
			name:        "unavailable checksum URL",
			checksumURL: srv.URL + "/unavailable",
			wantError:   "download checksum",
		},
		{
			name:        "malformed checksum file",
			checksumURL: srv.URL + "/malformed",
			wantError:   "invalid SHA-256 checksum",
		},
		{
			name:        "valid checksum file",
			checksumURL: srv.URL + "/valid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := checksumHits.Load()
			err := verifyArchiveChecksum(context.Background(), archivePath, tt.expectedChecksum, tt.checksumURL, t.TempDir())
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("verifyArchiveChecksum() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("verifyArchiveChecksum() error = %v, want %q", err, tt.wantError)
			}
			if tt.expectedChecksum != "" && checksumHits.Load() != before {
				t.Fatal("checksum URL was fetched despite an expected checksum being provided")
			}
		})
	}
}
