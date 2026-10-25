package zzz

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/elhenro/bee/internal/gitsafe"
)

// gitRun executes git in dir, captures stdout. stderr only on failure.
func gitRun(dir string, args ...string) (string, error) {
	cmd := gitsafe.Command(args...)
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

// MainRepoRoot resolves the main checkout behind dir. For a linked worktree
// that is the directory holding the shared git dir, so every worktree of one
// repo maps to one project key. Falls back to dir's own toplevel.
func MainRepoRoot(dir string) (string, error) {
	top, err := RepoRoot(dir)
	if err != nil {
		return "", err
	}
	common, err := gitRun(top, "rev-parse", "--git-common-dir")
	if err != nil {
		return top, nil
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(top, common)
	}
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common), nil
	}
	return top, nil
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

// AddAll stages everything.
func AddAll(dir string) error {
	_, err := gitRun(dir, "add", "-A")
	return err
}

// Commit creates ONE commit with msg. When sign is false (gnhf parity) the
// command is invoked with overrides that disable any configured signing —
// the run does NOT touch the user's git config. Returns the new short sha.
func Commit(dir, msg string, sign, noVerify bool) (string, error) {
	var args []string
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

// CommitAll stages every tracked + untracked change and commits. The
// atomicity is the VCS's own (index set then commit). Returns the new short sha.
func CommitAll(dir, msg string, sign, noVerify bool) (string, error) {
	if err := AddAll(dir); err != nil {
		return "", err
	}
	return Commit(dir, msg, sign, noVerify)
}

// ResetHard discards uncommitted changes back to HEAD.
func ResetHard(dir string) error {
	_, err := gitRun(dir, "reset", "--hard", "HEAD")
	return err
}

// CleanFD removes untracked files + dirs. Pairs with ResetHard for a full
// rollback after a failed iteration.
func CleanFD(dir string) error {
	_, err := gitRun(dir, "clean", "-fd")
	return err
}

// DiffStat returns the one-line `--shortstat` summary for HEAD~1..HEAD.
// Returns empty string when the commit didn't actually change anything.
func DiffStat(dir string) (string, error) {
	return gitRun(dir, "diff", "--shortstat", "HEAD~1..HEAD")
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
