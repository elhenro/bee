package config

import (
	"os"
	"path/filepath"
)

// ConfigPath returns the resolved path to bee's TOML config.
// Override with BEE_CONFIG; otherwise ~/.bee/config.toml.
func ConfigPath() string {
	if p := os.Getenv("BEE_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// fall back to cwd-relative; load.go handles a missing file gracefully
		return ".bee/config.toml"
	}
	return filepath.Join(home, ".bee", "config.toml")
}
