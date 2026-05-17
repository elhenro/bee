package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// DefaultDir returns ~/.bee/auth, creating it 0700 if missing.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".bee", "auth")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// SaveToken writes tok to <dir>/<provider>.json with 0600 perms.
func SaveToken(dir, provider string, tok *Token) error {
	if tok == nil {
		return errors.New("nil token")
	}
	p := filepath.Join(dir, provider+".json")
	b, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}

// LoadToken reads a previously saved token. Returns (nil, nil) if absent.
func LoadToken(dir, provider string) (*Token, error) {
	p := filepath.Join(dir, provider+".json")
	b, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var tok Token
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// DeleteToken removes the token file. No-op if missing.
func DeleteToken(dir, provider string) error {
	p := filepath.Join(dir, provider+".json")
	err := os.Remove(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
