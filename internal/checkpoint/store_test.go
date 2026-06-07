package checkpoint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newStore opens a store rooted at a temp project dir with a hermetic BEE_HOME.
func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	t.Setenv("BEE_HOME", t.TempDir())
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s, root
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenInitsShadowRepoAndExclude(t *testing.T) {
	s, _ := newStore(t)
	if _, err := os.Stat(filepath.Join(s.gitDir, "HEAD")); err != nil {
		t.Fatalf("shadow repo not initialized: %v", err)
	}
	excl, err := os.ReadFile(filepath.Join(s.gitDir, "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if !strings.Contains(string(excl), "/.git/") {
		t.Fatalf("exclude missing /.git/: %q", excl)
	}
}

func TestSnapshotChangesAndDedup(t *testing.T) {
	s, root := newStore(t)
	write(t, filepath.Join(root, "a.txt"), "one")
	sha1, err := s.Snapshot("m1", "")
	if err != nil {
		t.Fatalf("snap m1: %v", err)
	}
	write(t, filepath.Join(root, "a.txt"), "two")
	write(t, filepath.Join(root, "b.txt"), "bee")
	sha2, err := s.Snapshot("m2", "")
	if err != nil {
		t.Fatalf("snap m2: %v", err)
	}
	if sha1 == sha2 {
		t.Fatalf("expected distinct shas, got %s twice", sha1)
	}
	if got, ok := s.SnapshotFor("m1"); !ok || got != sha1 {
		t.Fatalf("SnapshotFor m1 = %q,%v want %s", got, ok, sha1)
	}
	stat, err := s.DiffStat(sha1, sha2)
	if err != nil || stat == "" {
		t.Fatalf("diffstat = %q err %v", stat, err)
	}

	// unchanged tree reuses the same commit for a new message id.
	sha3, err := s.Snapshot("m3", "")
	if err != nil {
		t.Fatalf("snap m3: %v", err)
	}
	if sha3 != sha2 {
		t.Fatalf("nothing-changed snapshot should reuse %s, got %s", sha2, sha3)
	}
}

func TestSnapshotEmptyDir(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Snapshot("m1", ""); err != nil {
		t.Fatalf("snapshot empty dir: %v", err)
	}
}

func TestDotGitNeverStaged(t *testing.T) {
	s, root := newStore(t)
	write(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	write(t, filepath.Join(root, "real.txt"), "hi")
	if _, err := s.Snapshot("m1", ""); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	files, err := s.git("ls-files")
	if err != nil {
		t.Fatalf("ls-files: %v", err)
	}
	if strings.Contains(files, ".git/") {
		t.Fatalf("real .git was snapshotted:\n%s", files)
	}
	if !strings.Contains(files, "real.txt") {
		t.Fatalf("expected real.txt tracked, got:\n%s", files)
	}
}
