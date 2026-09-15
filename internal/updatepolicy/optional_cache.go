package updatepolicy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	optionalCacheFile = "optional_update_status.json"
	optionalCacheDir  = ".thunder"
	optionalCacheSub  = "cache"
	OptionalUpdateTTL = time.Hour
)

type optionalCachePayload struct {
	Version     string    `json:"version"`
	LastAttempt time.Time `json:"last_attempt"`
}

func ReadOptionalUpdateAttempt(version string) (time.Time, error) {
	path, err := optionalCachePath()
	if err != nil {
		return time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	var payload optionalCachePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return time.Time{}, err
	}
	if payload.Version == "" || payload.Version != normalizeVersion(version) {
		return time.Time{}, nil
	}
	return payload.LastAttempt, nil
}

func WriteOptionalUpdateAttempt(version string, ts time.Time) error {
	path, err := optionalCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := optionalCachePayload{
		Version:     normalizeVersion(version),
		LastAttempt: ts.UTC(),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ClearOptionalUpdateAttempt removes the optional update cache entry, if it exists.
func ClearOptionalUpdateAttempt() error {
	path, err := optionalCachePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func optionalCachePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, optionalCacheDir, optionalCacheSub)
	return filepath.Join(dir, optionalCacheFile), nil
}
