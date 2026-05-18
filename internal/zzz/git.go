package zzz

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitRun executes git in dir, captures stdout. stderr only on failure.
func gitRun(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RepoRoot resolves dir's repo root via `git rev-parse --show-toplevel`.
func RepoRoot(dir string) (string, error) {
	return gitRun(dir, "rev-parse", "--show-toplevel")
}

// IsClean returns true when `git status --porcelain` has no output.
func IsClean(dir string) (bool, error) {
	out, err := gitRun(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// CurrentBranch returns the active branch name. Detached HEAD returns "HEAD".
func CurrentBranch(dir string) (string, error) {
	return gitRun(dir, "rev-parse", "--abbrev-ref", "HEAD")
}

// HeadSHA returns the short HEAD sha.
func HeadSHA(dir string) (string, error) {
	return gitRun(dir, "rev-parse", "--short", "HEAD")
}

// CreateBranchAndSwitch creates `name` and checks it out. Errors if it exists.
func CreateBranchAndSwitch(dir, name string) error {
	_, err := gitRun(dir, "switch", "-c", name)
	return err
}

// HasBranch returns true when `name` exists locally.
func HasBranch(dir, name string) bool {
	_, err := gitRun(dir, "rev-parse", "--verify", "refs/heads/"+name)
	return err == nil
}

// AddAll stages everything.
func AddAll(dir string) error {
	_, err := gitRun(dir, "add", "-A")
	return err
}

// Commit creates ONE commit with msg. When sign is false (gnhf parity) the
// command is invoked with overrides that disable any configured signing —
// the run does NOT touch the user's git config. Returns the new short sha.
func Commit(dir, msg string, sign, noVerify bool) (string, error) {
	args := []string{}
	if !sign {
		args = append(args, "-c", "commit.gpgsign=false", "-c", "gpg.format=")
	}
	args = append(args, "commit", "-m", msg)
	if noVerify {
		args = append(args, "--no-verify")
	}
	if _, err := gitRun(dir, args...); err != nil {
		return "", err
	}
	return HeadSHA(dir)
}

// CommitAll stages every tracked + untracked change and commits in a single
// git invocation. The atomicity is git's own (index set then commit). Returns
// the new short sha.
func CommitAll(dir, msg string, sign, noVerify bool) (string, error) {
	if _, err := gitRun(dir, "add", "-A"); err != nil {
		return "", err
	}
	args := []string{}
	if !sign {
		args = append(args, "-c", "commit.gpgsign=false", "-c", "gpg.format=")
	}
	args = append(args, "commit", "-m", msg)
	if noVerify {
		args = append(args, "--no-verify")
	}
	if _, err := gitRun(dir, args...); err != nil {
		return "", err
	}
	return HeadSHA(dir)
}

// ResetHard discards uncommitted changes back to ref (defaults to HEAD).
func ResetHard(dir, ref string) error {
	if ref == "" {
		ref = "HEAD"
	}
	_, err := gitRun(dir, "reset", "--hard", ref)
	return err
}

// CleanFD removes untracked files + dirs. Pairs with ResetHard for a full
// rollback after a failed iteration.
func CleanFD(dir string) error {
	_, err := gitRun(dir, "clean", "-fd")
	return err
}

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

// Fetch updates remote-tracking refs. remote defaults to "origin".
// Best-effort: not having a remote configured is not fatal for local-only flows.
func Fetch(dir, remote string) error {
	if remote == "" {
		remote = "origin"
	}
	_, err := gitRun(dir, "fetch", remote)
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

// IsRebasing returns true when a rebase is in progress in dir. Checks for
// .git/rebase-merge and .git/rebase-apply via git status --porcelain=v2.
func IsRebasing(dir string) bool {
	out, err := gitRun(dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return false
	}
	return strings.Contains(out, "# branch.head (no branch)") ||
		strings.Contains(out, "rebase")
}

// MergeFF fast-forwards branch onto base in dir. dir should be the main repo
// (not the worktree). Returns error if FF is not possible.
func MergeFF(dir, base, branch string) error {
	if base == "" {
		return errors.New("merge-ff: empty base")
	}
	if branch == "" {
		return errors.New("merge-ff: empty branch")
	}
	// switch base, then merge --ff-only branch
	if _, err := gitRun(dir, "switch", base); err != nil {
		return err
	}
	_, err := gitRun(dir, "merge", "--ff-only", branch)
	return err
}

// DiffStat returns the one-line `--shortstat` summary for HEAD~1..HEAD.
// Returns empty string when the commit didn't actually change anything.
func DiffStat(dir, ref string) (string, error) {
	if ref == "" {
		ref = "HEAD~1..HEAD"
	}
	return gitRun(dir, "diff", "--shortstat", ref)
}

// CommitMessageFrom builds a conventional commit subject + body. Subject is
// the first non-empty line of finalText truncated to 70 chars and lowercased
// "fix:" / "feat:" prefix when missing. Body carries the remainder.
func CommitMessageFrom(finalText string, iter, inTok, outTok int) string {
	lines := strings.Split(strings.TrimSpace(finalText), "\n")
	var subject string
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" {
			subject = s
			break
		}
	}
	if subject == "" {
		subject = fmt.Sprintf("zzz: iter %d", iter)
	}
	subject = strings.TrimPrefix(subject, "# ")
	if r := []rune(subject); len(r) > 70 {
		subject = string(r[:67]) + "..."
	}
	if !looksConventional(subject) {
		subject = "chore: " + subject
	}
	body := strings.TrimSpace(strings.Join(lines[1:], "\n"))
	footer := fmt.Sprintf("zzz-iter: %d\nzzz-tokens: %d in / %d out", iter, inTok, outTok)
	if body != "" {
		return subject + "\n\n" + body + "\n\n" + footer
	}
	return subject + "\n\n" + footer
}

// SaveBlockedPatch dumps the current tracked diff + porcelain status to
// ~/.bee/zzz/runs/<id>/blocked-<iter>.patch so the operator can inspect what
// the BLOCKED iteration produced before the run reset the tree. Untracked
// files are listed by name only (no content) to keep the patch self-explanatory.
func SaveBlockedPatch(id string, iter int, dir string) error {
	runDir, err := RunDir(id)
	if err != nil {
		return err
	}
	status, _ := gitRun(dir, "status", "--porcelain")
	diff, _ := gitRun(dir, "diff", "HEAD")
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# zzz blocked-iter %d\n# status (porcelain):\n", iter))
	if status == "" {
		b.WriteString("# (clean)\n")
	} else {
		b.WriteString(status)
		b.WriteString("\n")
	}
	b.WriteString("\n# diff HEAD (tracked changes only):\n")
	b.WriteString(diff)
	b.WriteString("\n")
	path := filepath.Join(runDir, fmt.Sprintf("blocked-%d.patch", iter))
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// looksConventional checks if subject already has a conventional prefix so
// CommitMessageFrom doesn't double-stamp "chore: feat: ...".
func looksConventional(subject string) bool {
	for _, p := range []string{"feat", "fix", "chore", "docs", "refactor", "test", "perf", "build", "ci", "style", "revert"} {
		if strings.HasPrefix(subject, p+":") || strings.HasPrefix(subject, p+"(") {
			return true
		}
	}
	return false
}
