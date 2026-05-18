// Package worktree creates ephemeral `git worktree` checkouts so concurrent
// agent workers can each mutate files without racing on one shared tree.
//
// Usage:
//
//	wt, err := worktree.Create(repoRoot, "swarm-w0")
//	if err != nil { ... }
//	defer wt.Cleanup()
//	// wt.Path is a fresh working tree on a detached HEAD at the original ref.
//
// The caller decides what to do with diverging worktrees on completion
// (merge, copy patches back, discard). This package only handles
// allocation + cleanup; it does not invent a merge policy.
package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Worktree is one isolated checkout. Path is the working directory; the
// underlying branch is detached so commits there don't move the source
// repo's HEAD.
type Worktree struct {
	Path     string
	repoRoot string
	branch   string
}

// Cleanup removes the worktree and its on-disk state. Best-effort: errors
// are returned but the directory is also force-removed so a partial failure
// can't leak storage indefinitely.
func (w *Worktree) Cleanup() error {
	if w == nil || w.Path == "" {
		return nil
	}
	cmd := exec.Command("git", "-C", w.repoRoot, "worktree", "remove", "--force", w.Path)
	out, err := cmd.CombinedOutput()
	// fall through to RemoveAll either way; `git worktree remove` will fail
	// if a worker left local commits, and we still want the disk space back.
	rmErr := os.RemoveAll(w.Path)
	if err != nil {
		// `git worktree remove` non-zero is non-fatal once we've nuked the
		// dir, but surface the original combined output for diagnostics.
		return fmt.Errorf("git worktree remove: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return rmErr
}

// Create allocates a new worktree rooted under the repo's standard
// `.git/worktrees` parent dir if available, falling back to a sibling
// directory of repoRoot. `label` is a short suffix used to name both the
// directory and the detached branch reference for easier debugging.
//
// repoRoot must point inside a git repo. Returns an error if `git` is not
// on PATH or repoRoot is not a working tree.
func Create(repoRoot, label string) (*Worktree, error) {
	if repoRoot == "" {
		return nil, errors.New("worktree: empty repoRoot")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil, fmt.Errorf("worktree: git not on PATH: %w", err)
	}
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	parent := filepath.Join(abs, ".git", "bee-worktrees")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		// .git may be a file (submodule, secondary worktree). Fall back to
		// a sibling temp dir so the call still works.
		parent, err = os.MkdirTemp(filepath.Dir(abs), "bee-worktree-")
		if err != nil {
			return nil, err
		}
	}
	dir, err := os.MkdirTemp(parent, label+"-")
	if err != nil {
		return nil, err
	}
	// remove dir before `git worktree add` so git creates it cleanly.
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "-C", abs, "worktree", "add", "--detach", dir, "HEAD")
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return &Worktree{Path: dir, repoRoot: abs, branch: label}, nil
}
