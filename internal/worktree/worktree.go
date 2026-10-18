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
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/elhenro/bee/internal/gitsafe"
)

// Worktree is one isolated checkout. Path is the working directory; the
// underlying branch is detached so commits there don't move the source
// repo's HEAD.
type Worktree struct {
	Path     string
	repoRoot string
	branch   string
	base     string // HEAD sha the worktree was cut from
}

// Cleanup removes the worktree and its on-disk state. Best-effort: errors
// are returned but the directory is also force-removed so a partial failure
// can't leak storage indefinitely.
func (w *Worktree) Cleanup() error {
	if w == nil || w.Path == "" {
		return nil
	}
	cmd := gitsafe.Command("-C", w.repoRoot, "worktree", "remove", "--force", w.Path)
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

// MergeBack applies every change made inside the worktree onto the source
// repo's working tree. It stages the worktree (so new/untracked files appear),
// diffs against the commit the worktree was cut from (so commits the worker
// made count too), and applies that patch in repoRoot with a 3-way merge.
// Returns nil when the worktree made no changes. A non-nil error means the
// patch did not apply cleanly (conflicting edits in the source tree) — the
// caller decides whether to drop the worker.
//
// Not safe to call concurrently for two worktrees over the same repo: the
// caller must serialize merges (git index is per-repo).
func (w *Worktree) MergeBack() error {
	if w == nil || w.Path == "" {
		return nil
	}
	if out, err := gitsafe.Command("-C", w.Path, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	patch, err := gitsafe.Command("-C", w.Path, "diff", "--cached", "--binary", w.base).Output()
	if err != nil {
		return fmt.Errorf("git diff: %w", err)
	}
	if len(strings.TrimSpace(string(patch))) == 0 {
		return nil // worker changed nothing
	}
	cmd := gitsafe.Command("-C", w.repoRoot, "apply", "--3way", "--whitespace=nowarn")
	cmd.Stdin = bytes.NewReader(patch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git apply: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
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
	// repoRoot may be a subdir or a linked worktree: ask git for the real
	// toplevel and common dir instead of assuming <repoRoot>/.git.
	top, err := gitOut(abs, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("worktree: %w", err)
	}
	base, err := gitOut(top, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("worktree: %w", err)
	}
	parent := filepath.Join(top, ".git", "bee-worktrees")
	if common, cerr := gitOut(top, "rev-parse", "--git-common-dir"); cerr == nil {
		if !filepath.IsAbs(common) {
			common = filepath.Join(top, common)
		}
		parent = filepath.Join(common, "bee-worktrees")
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		// fall back to a sibling temp dir so the call still works.
		parent, err = os.MkdirTemp(filepath.Dir(top), "bee-worktree-")
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
	cmd := gitsafe.Command("-C", top, "worktree", "add", "--detach", dir, base)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("git worktree add: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return &Worktree{Path: dir, repoRoot: top, branch: label, base: base}, nil
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := gitsafe.Command(append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
