package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/zzz"
)

func zzzTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("BEE_HOME", t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "t"},
		{"config", "user.email", "t@t"},
		{"config", "commit.gpgsign", "false"},
		{"commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	t.Chdir(dir)
	return dir
}

// an error return used to nil the named unlock before the deferred release ran
func TestStartRunDirtyTreeErrorsWithoutPanic(t *testing.T) {
	dir := zzzTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, unlock, err := startRun(zzz.Config{Objective: "x"})
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("want dirty-tree error, got run=%v err=%v", r, err)
	}
	if unlock != nil {
		t.Fatal("failed start must not hand back a lock")
	}
	// the lock was released on the way out, so a second start sees the same error
	if _, _, err := startRun(zzz.Config{Objective: "x"}); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("second start: want dirty-tree error, got %v", err)
	}
}

func TestResumeRunBranchMismatchErrorsWithoutPanic(t *testing.T) {
	dir := zzzTestRepo(t)
	r := &zzz.Run{ID: zzz.NewID(), RepoRoot: dir, Mode: zzz.ModeCurrent, Branch: "feature", Status: zzz.StatusAborted}
	if err := zzz.SaveMeta(r); err != nil {
		t.Fatal(err)
	}
	_, unlock, err := resumeRun(r.ID)
	if err == nil || !strings.Contains(err.Error(), "checked out") {
		t.Fatalf("want branch-mismatch error, got %v", err)
	}
	if unlock != nil {
		t.Fatal("failed resume must not hand back a lock")
	}
}
