package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMergeBack_CountsWorkerCommits(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := initRepo(t)
	wt, err := Create(repo, "commit-w")
	if err != nil {
		t.Fatal(err)
	}
	defer wt.Cleanup()
	if err := os.WriteFile(filepath.Join(wt.Path, "done.txt"), []byte("committed"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "worker"}} {
		if out, err := exec.Command("git", append([]string{"-C", wt.Path}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := wt.MergeBack(); err != nil {
		t.Fatalf("MergeBack: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(repo, "done.txt"))
	if err != nil || string(got) != "committed" {
		t.Fatalf("committed file in source = %q, err=%v", got, err)
	}
}

func TestMergeBack_FromSubdir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := initRepo(t)
	sub := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	wt, err := Create(sub, "sub-w")
	if err != nil {
		t.Fatalf("Create from subdir: %v", err)
	}
	defer wt.Cleanup()
	if _, err := os.Stat(filepath.Join(sub, ".git")); err == nil {
		t.Fatal("subdir grew a .git")
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "README.md"), []byte("outside subdir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := wt.MergeBack(); err != nil {
		t.Fatalf("MergeBack: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(repo, "README.md"))
	if string(got) != "outside subdir" {
		t.Fatalf("README.md in source = %q", got)
	}
}
