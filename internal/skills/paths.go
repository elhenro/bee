package skills

import (
	"os"
	"path/filepath"
)

// env overrides for tests / advanced users
const (
	envBaseDir = "BEE_SKILLS_DIR"
	envHome    = "BEE_HOME"
)

// BaseDir returns the directory holding skill *.md files.
// Default ~/.bee/skills. Override via BEE_SKILLS_DIR or BEE_HOME.
func BaseDir() string {
	if v := os.Getenv(envBaseDir); v != "" {
		return v
	}
	return filepath.Join(beeHome(), "skills")
}

func beeHome() string {
	if v := os.Getenv(envHome); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// last-resort fallback, will surface in caller errors
		return ".bee"
	}
	return filepath.Join(home, ".bee")
}
