package config

import (
	"os"
	"path/filepath"

	"github.com/elhenro/bee/internal/beehome"
)

// ConfigPath returns the resolved path to bee's TOML config.
// Override with BEE_CONFIG; otherwise $BEE_HOME or ~/.bee, plus config.toml.
func ConfigPath() string {
	if p := os.Getenv("BEE_CONFIG"); p != "" {
		return p
	}
	root, err := beehome.Dir()
	if err != nil {
		// fall back to cwd-relative; load.go handles a missing file gracefully
		return ".bee/config.toml"
	}
	return filepath.Join(root, "config.toml")
}
