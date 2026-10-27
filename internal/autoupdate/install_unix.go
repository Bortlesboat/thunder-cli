//go:build !windows

package autoupdate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

type unixInstaller struct{}

func (u unixInstaller) Install(ctx context.Context, exe, newBinary, version string, src Source) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	if !dirWritable(dir) {
		if err := installWithSudo(ctx, newBinary, exe, version); err != nil {
			return err
		}
		return nil
	}

	tmpTarget := filepath.Join(dir, ".tnr-tmp")
	if err := copyFile(newBinary, tmpTarget); err != nil {
		return err
	}
	if err := os.Chmod(tmpTarget, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpTarget, exe); err != nil {
		if isPermissionError(err) {
			if err := installWithSudo(ctx, newBinary, exe, version); err != nil {
				return err
			}
			return nil
		}
		return fmt.Errorf("install failed: %w", err)
	}
	fmt.Printf("Successfully updated to version %s\n", version)
	return nil
}

func installWithSudo(ctx context.Context, newBinary, exe, version string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tempDir := filepath.Dir(newBinary)
	tmpFile, err := os.CreateTemp(tempDir, "tnr-sudo-*")
	if err != nil {
		return err
	}
	tmpTarget := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpTarget)
		return err
	}
	defer os.Remove(tmpTarget)

	if err := copyFile(newBinary, tmpTarget); err != nil {
		return err
	}
	if err := os.Chmod(tmpTarget, 0o755); err != nil {
		return err
	}

	args := []string{"mv", tmpTarget, exe}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		args = append([]string{"-n"}, args...)
	}
	fmt.Fprintf(os.Stderr, "Updating %s requires administrator permission.\n", exe)
	cmd := exec.CommandContext(ctx, "sudo", args...)
	cmd.WaitDelay = time.Second
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			detail = ": " + detail
		}
		return fmt.Errorf("sudo install failed: installing %q: %w%s\nRun 'tnr update' in an interactive terminal with an account allowed to use sudo, or reinstall tnr in a user-writable directory", exe, err, detail)
	}
	fmt.Printf("Successfully updated to version %s\n", version)
	return nil
}

func isPermissionError(err error) bool {
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			return errno == syscall.EACCES || errno == syscall.EPERM
		}
	}
	return false
}
