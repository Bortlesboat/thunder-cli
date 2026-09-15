package credentials

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var inProcessLock = make(chan struct{}, 1)

func withFileLock(ctx context.Context, configPath string, fn func() error) error {
	select {
	case inProcessLock <- struct{}{}:
		defer func() { <-inProcessLock }()
	case <-ctx.Done():
		return ctx.Err()
	}

	lockPath := configPath + ".lock"
	lockDir := filepath.Dir(lockPath)
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return fmt.Errorf("create credential lock directory: %w", err)
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		return fmt.Errorf("secure credential lock directory: %w", err)
	}
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open credential lock: %w", err)
	}
	defer lockFile.Close()
	if err := lockFile.Chmod(0o600); err != nil {
		return fmt.Errorf("secure credential lock: %w", err)
	}

	for {
		err = tryLockFile(lockFile)
		if err == nil {
			break
		}
		if !errors.Is(err, errLockBusy) {
			return fmt.Errorf("acquire credential lock: %w", err)
		}

		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	defer func() { _ = unlockFile(lockFile) }()

	return fn()
}
