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
