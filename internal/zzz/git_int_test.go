package zzz

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// commitFile writes content and commits it on the current branch of dir.
func commitFile(t *testing.T, dir, name, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", msg}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zzz-test", "GIT_AUTHOR_EMAIL=zzz@test",
			"GIT_COMMITTER_NAME=zzz-test", "GIT_COMMITTER_EMAIL=zzz@test")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

func TestDiffAgainstBaseSHA(t *testing.T) {
	repo := newRepo(t)
	base, err := HeadSHA(repo)
	if err != nil {
		t.Fatal(err)
	}
	commitFile(t, repo, "feature.txt", "new feature line", "feat: add feature")

	diff, err := DiffAgainst(repo, base, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "new feature line") || !strings.Contains(diff, "feature.txt") {
		t.Fatalf("diff against base SHA missing new work: %q", diff)
	}
}

func TestDiffAgainstTruncates(t *testing.T) {
	repo := newRepo(t)
	base, _ := HeadSHA(repo)
	big := make([]byte, 0, 9000)
	for i := 0; i < 9000; i++ {
		big = append(big, 'a')
	}
	commitFile(t, repo, "big.txt", string(big), "big")
	diff, err := DiffAgainst(repo, base, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff) > 600 || !strings.Contains(diff, "diff truncated") {
		t.Fatalf("expected truncated diff, got len=%d", len(diff))
	}
}

func TestMainRepoRootFromLinkedWorktree(t *testing.T) {
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	if err := WorktreeAdd(repo, wt, "zzz/mrr"); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(repo)
	for _, dir := range []string{repo, wt} {
		got, err := MainRepoRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		if g, _ := filepath.EvalSymlinks(got); g != want {
			t.Fatalf("MainRepoRoot(%s) = %s, want %s", dir, g, want)
		}
	}
}

func TestMergeFFLeavesCheckoutAlone(t *testing.T) {
	repo := newRepo(t)
	base, _ := CurrentBranch(repo)
	if err := CreateBranchAt(repo, "feat/x", "HEAD"); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := WorktreeAdd(repo, wt, "agent/x"); err != nil {
		t.Fatal(err)
	}
	commitFile(t, wt, "a.txt", "a", "a")
	tip, _ := gitRun(wt, "rev-parse", "HEAD")

	// user sits on another branch: base moves by ref, checkout stays put
	if _, err := gitRun(repo, "switch", "-q", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if err := MergeFF(repo, base, tip); err != nil {
		t.Fatalf("MergeFF: %v", err)
	}
	if cur, _ := CurrentBranch(repo); cur != "feat/x" {
		t.Fatalf("checkout moved to %s", cur)
	}
	if got, _ := gitRun(repo, "rev-parse", base); got != tip {
		t.Fatalf("%s = %s, want %s", base, got, tip)
	}

	// base checked out in another worktree: a ref move would desync it
	other := filepath.Join(t.TempDir(), "other")
	if _, err := gitRun(repo, "worktree", "add", "-q", "-b", "side", other); err != nil {
		t.Fatal(err)
	}
	commitFile(t, other, "b.txt", "b", "b")
	side, _ := gitRun(other, "rev-parse", "HEAD")
	if _, err := gitRun(repo, "switch", "-q", base); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRun(repo, "switch", "-q", "feat/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitRun(other, "switch", "-q", base); err != nil {
		t.Fatal(err)
	}
	if err := MergeFF(repo, base, side); err == nil {
		t.Fatal("MergeFF must refuse when base is checked out elsewhere")
	}
}

func TestCreateBranchAt(t *testing.T) {
	repo := newRepo(t)
	commitFile(t, repo, "x.txt", "x", "x")
	tip, _ := HeadSHA(repo)
	if err := CreateBranchAt(repo, "zzz/queen-test", tip); err != nil {
		t.Fatal(err)
	}
	if !HasBranch(repo, "zzz/queen-test") {
		t.Fatal("review branch not created")
	}
	// idempotent: -f allows re-point
	if err := CreateBranchAt(repo, "zzz/queen-test", tip); err != nil {
		t.Fatalf("re-create should be idempotent: %v", err)
	}
}
