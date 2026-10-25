// Package agents drives the "bee agents" parallel-agent mode. Each agent
// runs in its own git worktree as a detached headless `bee run --bg-loop`
// process. The TUI overview spawns, lists, and merges them. State is
// file-based (bgreg) so the TUI can be killed and restarted without losing
// running agents.
package agents

import (
	"os"
	"path/filepath"

	"github.com/elhenro/bee/internal/beehome"
)

// beeDir returns the bee home subdir elem, created on demand.
func beeDir(elem ...string) (string, error) {
	p, err := beehome.Join(elem...)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// Root returns ~/.bee/agents (created on demand).
func Root() (string, error) { return beeDir("agents") }

// WorktreeRoot returns ~/.bee/agents/worktrees.
func WorktreeRoot() (string, error) { return beeDir("agents", "worktrees") }

// WorktreePath returns ~/.bee/agents/worktrees/<id>.
func WorktreePath(id string) (string, error) {
	r, err := WorktreeRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, id), nil
}

// MergeLockPath returns ~/.bee/agents/merge.lock.
func MergeLockPath() (string, error) {
	r, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(r, "merge.lock"), nil
}
