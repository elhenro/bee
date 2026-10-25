package skills

import (
	"os"
	"path/filepath"

	"github.com/elhenro/bee/internal/beehome"
)

// env overrides for tests / advanced users
const (
	envBaseDir = "BEE_SKILLS_DIR"
)

// BaseDir returns the directory holding skill *.md files.
// Default ~/.bee/skills. Override via BEE_SKILLS_DIR or BEE_HOME.
func BaseDir() string {
	if v := os.Getenv(envBaseDir); v != "" {
		return v
	}
	root, err := beehome.Dir()
	if err != nil {
		root = ".bee" // last-resort fallback, will surface in caller errors
	}
	return filepath.Join(root, "skills")
}
