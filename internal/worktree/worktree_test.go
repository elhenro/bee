package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo seeds a fresh repo with one commit so worktree add has a HEAD.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestCreate_ProducesIsolatedTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := initRepo(t)
	wt, err := Create(repo, "test-w")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer wt.Cleanup()

	if wt.Path == "" || wt.Path == repo {
		t.Fatalf("worktree path empty or equal to repo: %q", wt.Path)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "README.md")); err != nil {
		t.Fatalf("worktree missing seeded file: %v", err)
	}
	// mutation in worktree must not affect source.
	if err := os.WriteFile(filepath.Join(wt.Path, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "scratch.txt")); !os.IsNotExist(err) {
		t.Fatalf("mutation leaked into source repo: %v", err)
	}
}

func TestCleanup_RemovesPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := initRepo(t)
	wt, err := Create(repo, "test-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	path := wt.Path
	if err := wt.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir not removed: %v", err)
	}
}

func TestCreate_NotARepo(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(dir, "x"); err == nil {
		t.Fatalf("expected error in non-repo dir")
	}
}

func TestMergeBack_AppliesChangesToSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := initRepo(t)
	wt, err := Create(repo, "merge-w")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer wt.Cleanup()

	// new file + edit to an existing file inside the worktree.
	if err := os.WriteFile(filepath.Join(wt.Path, "new.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := wt.MergeBack(); err != nil {
		t.Fatalf("MergeBack: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(repo, "new.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("new.txt in source = %q, err=%v; want %q", got, err, "hello")
	}
	got, err = os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil || string(got) != "changed" {
		t.Fatalf("README.md in source = %q, err=%v; want %q", got, err, "changed")
	}
}

func TestMergeBack_NoChangesIsNoop(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := initRepo(t)
	wt, err := Create(repo, "noop-w")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer wt.Cleanup()
	if err := wt.MergeBack(); err != nil {
		t.Fatalf("MergeBack on clean worktree = %v, want nil", err)
	}
}
