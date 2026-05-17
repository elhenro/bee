package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// SaveAPIKey writes a plain-text api key to <dir>/<provider>.key with 0600
// perms. Used for providers that authenticate via static api keys (no oauth
// flow) when the user enters one through /login. Empty keys are rejected.
func SaveAPIKey(dir, provider, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("empty api key")
	}
	if dir == "" || provider == "" {
		return errors.New("missing dir or provider")
	}
	p := filepath.Join(dir, provider+".key")
	return os.WriteFile(p, []byte(key), 0o600)
}

// LoadAPIKey reads a previously saved api key. Returns ("", nil) if the file
// is absent — callers fall back to env / treat as unauthenticated.
func LoadAPIKey(dir, provider string) (string, error) {
	if dir == "" || provider == "" {
		return "", nil
	}
	p := filepath.Join(dir, provider+".key")
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// DeleteAPIKey removes the saved key file. No-op when missing.
func DeleteAPIKey(dir, provider string) error {
	if dir == "" || provider == "" {
		return nil
	}
	p := filepath.Join(dir, provider+".key")
	err := os.Remove(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// HasAPIKey reports whether a key file exists for provider (best-effort —
// errors are treated as "no key" so callers don't have to fan out error
// handling for a probe).
func HasAPIKey(dir, provider string) bool {
	if dir == "" || provider == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, provider+".key"))
	return err == nil
}
