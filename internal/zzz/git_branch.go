package zzz

import (
	"errors"
	"fmt"
	"strings"
)

// WorktreeAdd creates a new worktree at path tracking new branch.
// path MUST NOT exist; git refuses otherwise.
func WorktreeAdd(repoRoot, path, branch string) error {
	_, err := gitRun(repoRoot, "worktree", "add", "-b", branch, path)
	return err
}

// WorktreeRemove tears down a worktree. Force flag handles dirty trees.
func WorktreeRemove(repoRoot, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := gitRun(repoRoot, args...)
	return err
}

// Push pushes the named branch with upstream tracking.
func Push(dir, branch string) error {
	if branch == "" {
		return errors.New("push: empty branch")
	}
	_, err := gitRun(dir, "push", "-u", "origin", branch)
	return err
}

// Rebase replays HEAD commits onto onto. Returns the raw error including
// stderr on conflict so callers can surface it.
func Rebase(dir, onto string) error {
	if onto == "" {
		return errors.New("rebase: empty onto")
	}
	_, err := gitRun(dir, "rebase", onto)
	return err
}

// RebaseAbort aborts an in-progress rebase. Safe to call when no rebase is
// active — git exits non-zero but the caller can ignore it.
func RebaseAbort(dir string) error {
	_, err := gitRun(dir, "rebase", "--abort")
	return err
}

// MergeFF fast-forwards branch onto base in dir. dir should be the main repo
// (not the worktree). Returns error if FF is not possible. It never switches
// the user's checkout: when base is checked out in dir it merges in place,
// otherwise it moves the base ref directly (compare-and-swap) unless another
// worktree has base checked out, which a ref move would desync.
func MergeFF(dir, base, branch string) error {
	if base == "" {
		return errors.New("merge-ff: empty base")
	}
	if branch == "" {
		return errors.New("merge-ff: empty branch")
	}
	if cur, err := CurrentBranch(dir); err == nil && cur == base {
		_, err := gitRun(dir, "merge", "--ff-only", branch)
		return err
	}
	if at := checkedOutAt(dir, base); at != "" {
		return fmt.Errorf("merge-ff: %s is checked out at %s", base, at)
	}
	old, err := gitRun(dir, "rev-parse", "--verify", "refs/heads/"+base)
	if err != nil {
		return err
	}
	sha, err := gitRun(dir, "rev-parse", "--verify", branch+"^{commit}")
	if err != nil {
		return err
	}
	if _, err := gitRun(dir, "merge-base", "--is-ancestor", old, sha); err != nil {
		return fmt.Errorf("merge-ff: %s is not a fast-forward of %s", branch, base)
	}
	_, err = gitRun(dir, "update-ref", "-m", "bee: fast-forward "+base, "refs/heads/"+base, sha, old)
	return err
}

// checkedOutAt returns the path of the worktree that has branch checked out,
// or "" when none does.
func checkedOutAt(dir, branch string) string {
	out, err := gitRun(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	var path string
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case line == "branch refs/heads/"+branch:
			return path
		}
	}
	return ""
}

// DiffAgainst returns the full diff of dir's working tree (clean = HEAD)
// against ref. Used to judge one worker branch's whole contribution: with a
// committed tree this equals ref..HEAD. Bounded by maxBytes (0 = unbounded);
// over the cap the tail is dropped and a truncation marker appended so a huge
// branch can't blow the judge's token budget.
func DiffAgainst(dir, ref string, maxBytes int) (string, error) {
	out, err := gitRun(dir, "diff", ref)
	if err != nil {
		return "", err
	}
	if maxBytes > 0 && len(out) > maxBytes {
		out = out[:maxBytes] + "\n…[diff truncated]"
	}
	return out, nil
}

// CreateBranchAt creates name pointing at start without checking it out.
// -f overwrites an existing ref so a re-run of queen can refresh the
// consolidated review branch.
func CreateBranchAt(dir, name, start string) error {
	_, err := gitRun(dir, "branch", "-f", name, start)
	return err
}
