package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
)

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestRestoreRevertsAndIsReversible(t *testing.T) {
	s, root := newStore(t)
	a := filepath.Join(root, "a.txt")
	write(t, a, "one")
	if _, err := s.Snapshot("m1", ""); err != nil {
		t.Fatalf("snap m1: %v", err)
	}

	// later turn: modify a, create b.
	write(t, a, "two")
	b := filepath.Join(root, "b.txt")
	write(t, b, "bee")
	if _, err := s.Snapshot("m2", ""); err != nil {
		t.Fatalf("snap m2: %v", err)
	}

	res, err := s.Restore("m1")
	if err != nil {
		t.Fatalf("restore m1: %v", err)
	}
	if got := read(t, a); got != "one" {
		t.Fatalf("a.txt = %q want one", got)
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatalf("b.txt should be removed, stat err = %v", err)
	}
	if res.UndoSHA == "" {
		t.Fatalf("expected an undo sha for reversibility")
	}

	// the undo ref captured the m2 state; restoring it brings b back.
	if _, err := s.Restore("m2"); err != nil {
		t.Fatalf("restore m2: %v", err)
	}
	if got := read(t, a); got != "two" {
		t.Fatalf("a.txt after re-restore = %q want two", got)
	}
	if got := read(t, b); got != "bee" {
		t.Fatalf("b.txt after re-restore = %q want bee", got)
	}
}

func TestRestoreKeepsGitignored(t *testing.T) {
	s, root := newStore(t)
	write(t, filepath.Join(root, ".gitignore"), "secret.env\n")
	write(t, filepath.Join(root, "code.txt"), "v1")
	if _, err := s.Snapshot("m1", ""); err != nil {
		t.Fatalf("snap m1: %v", err)
	}

	// gitignored file created after the snapshot must survive a rewind.
	secret := filepath.Join(root, "secret.env")
	write(t, secret, "TOKEN=abc")
	write(t, filepath.Join(root, "code.txt"), "v2")
	if _, err := s.Snapshot("m2", ""); err != nil {
		t.Fatalf("snap m2: %v", err)
	}

	if _, err := s.Restore("m1"); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if got := read(t, secret); got != "TOKEN=abc" {
		t.Fatalf("gitignored secret.env should persist, got %q", got)
	}
	if got := read(t, filepath.Join(root, "code.txt")); got != "v1" {
		t.Fatalf("code.txt = %q want v1", got)
	}
}

func TestListNewestFirst(t *testing.T) {
	s, root := newStore(t)
	write(t, filepath.Join(root, "f"), "1")
	if _, err := s.Snapshot("m1", "first"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "f"), "2")
	if _, err := s.Snapshot("m2", "second"); err != nil {
		t.Fatal(err)
	}
	snaps, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("want 2 snapshots, got %d", len(snaps))
	}
	if snaps[0].MsgID != "m2" {
		t.Fatalf("newest-first expected m2, got %s", snaps[0].MsgID)
	}
}
