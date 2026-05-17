// Package agents drives the "bee agents" parallel-agent mode. Each agent
// runs in its own git worktree as a detached headless `bee run --bg-loop`
// process. The TUI overview spawns, lists, and merges them. State is
// file-based (bgreg) so the TUI can be killed and restarted without losing
// running agents.
package agents

import (
	"os"
	"path/filepath"
)

// envBeeHome lets tests redirect ~/.bee.
const envBeeHome = "BEE_HOME"

func beeHome() (string, error) {
	if v := os.Getenv(envBeeHome); v != "" {
		return v, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".bee"), nil
}

// Root returns ~/.bee/agents (created on demand).
func Root() (string, error) {
	h, err := beeHome()
	if err != nil {
		return "", err
	}
	p := filepath.Join(h, "agents")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// WorktreeRoot returns ~/.bee/agents/worktrees.
func WorktreeRoot() (string, error) {
	r, err := Root()
	if err != nil {
		return "", err
	}
	p := filepath.Join(r, "worktrees")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

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
