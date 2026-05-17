// First-run init: ensure ~/.bee/skills exists and seed bundled skills
// when the user has none. Silent on success; never fatal.
package main

import (
	"os"
	"path/filepath"

	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/skills/bundled"
)

// beeHomeDirs returns the standard ~/.bee subdirectories that init touches.
// honors BEE_HOME for tests / alt installs.
func beeHomeDirs() (skillsDir, memoryDir, sessionsDir string) {
	skillsDir = skills.BaseDir()
	base := filepath.Dir(skillsDir) // ~/.bee
	if v := os.Getenv("BEE_HOME"); v != "" {
		base = v
	}
	memoryDir = filepath.Join(base, "memory")
	sessionsDir = filepath.Join(base, "sessions")
	return
}

// ensureFirstRun is best-effort. Called from `bee run` and TUI startup.
// Silent on success, never fatal: a broken HOME shouldn't block the agent.
func ensureFirstRun() {
	skillsDir, _, _ := beeHomeDirs()
	if !dirEmptyOrMissing(skillsDir) {
		return
	}
	_, _ = bundled.WriteDefaults(skillsDir)
}

// dirEmptyOrMissing returns true when the dir doesn't exist or contains
// zero entries. We seed defaults in both cases.
func dirEmptyOrMissing(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	return len(entries) == 0
}
