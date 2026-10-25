package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGitFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGitBranchUncached(t *testing.T) {
	root := t.TempDir()

	// plain repo, asked from a nested directory
	repo := filepath.Join(root, "repo")
	writeGitFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/feature/x\n")
	nested := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := gitBranchUncached(nested); got != "feature/x" {
		t.Errorf("branch = %q, want feature/x", got)
	}

	// detached head shows a short sha
	detached := filepath.Join(root, "detached")
	writeGitFile(t, filepath.Join(detached, ".git", "HEAD"), "0123456789abcdef\n")
	if got := gitBranchUncached(detached); got != "0123456" {
		t.Errorf("detached = %q, want 0123456", got)
	}

	// worktree: .git is a pointer file, relative or absolute
	gitDir := filepath.Join(root, "main", ".git", "worktrees", "w")
	writeGitFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/wt\n")
	wtAbs := filepath.Join(root, "wt-abs")
	writeGitFile(t, filepath.Join(wtAbs, ".git"), "gitdir: "+gitDir+"\n")
	if got := gitBranchUncached(wtAbs); got != "wt" {
		t.Errorf("absolute pointer = %q, want wt", got)
	}
	wtRel := filepath.Join(root, "wt-rel")
	writeGitFile(t, filepath.Join(wtRel, ".git"), "gitdir: ../main/.git/worktrees/w\n")
	if got := gitBranchUncached(wtRel); got != "wt" {
		t.Errorf("relative pointer = %q, want wt", got)
	}

	// junk pointer file and no repo at all
	junk := filepath.Join(root, "junk")
	writeGitFile(t, filepath.Join(junk, ".git"), "not a pointer\n")
	if got := gitBranchUncached(junk); got != "" {
		t.Errorf("junk pointer = %q, want empty", got)
	}
	if got := gitBranchUncached(""); got != "" {
		t.Errorf("empty cwd = %q, want empty", got)
	}
}
