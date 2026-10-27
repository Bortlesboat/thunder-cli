//go:build !windows

package autoupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSudoInstallFailure(t *testing.T) {
	for _, tt := range []struct {
		name   string
		script string
		want   string
	}{
		{"password required", "echo 'sudo: a password is required' >&2; exit 1", "sudo: a password is required"},
		{"permission denied", "echo 'sudo: not allowed to execute mv' >&2; exit 1", "sudo: not allowed to execute mv"},
		{"deadline", "exec /bin/sleep 5", "context deadline exceeded"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte("#!/bin/sh\n"+tt.script+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			exe, replacement := filepath.Join(dir, "tnr"), filepath.Join(dir, "replacement")
			for path, data := range map[string]string{exe: "old binary", replacement: "new binary"} {
				if err := os.WriteFile(path, []byte(data), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			timeout := 10 * time.Second
			if tt.name == "deadline" {
				timeout = time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			err := installWithSudo(ctx, replacement, exe, "9.9.9")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("install error = %v, want %q", err, tt.want)
			}
			if tt.name == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("deadline cause lost: %v", err)
			}
			data, err := os.ReadFile(exe)
			if err != nil || string(data) != "old binary" {
				t.Fatalf("failed update changed installed binary: %q, %v", data, err)
			}
			staged, err := filepath.Glob(filepath.Join(dir, "tnr-sudo-*"))
			if err != nil || len(staged) != 0 {
				t.Fatalf("failed update left staging files: %v, %v", staged, err)
			}
		})
	}
}
