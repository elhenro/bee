// Package session implements append-only JSONL rollouts plus parent-pointer
// tree reconstruction for bee sessions.
package session

import (
	"errors"
	"os"
	"path/filepath"
)

// SessionsDir returns the directory holding session JSONL files.
// Override via BEE_SESSIONS_DIR for tests / alt installs.
func SessionsDir() (string, error) {
	if v := os.Getenv("BEE_SESSIONS_DIR"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if home == "" {
		return "", errors.New("session: empty home dir")
	}
	return filepath.Join(home, ".bee", "sessions"), nil
}

// Path returns the JSONL file path for a session id.
func Path(id string) (string, error) {
	dir, err := SessionsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".jsonl"), nil
}

// ensureDir mkdirs the sessions directory if missing.
func ensureDir() (string, error) {
	dir, err := SessionsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}
