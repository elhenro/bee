package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInRoot_SymlinkedRoot(t *testing.T) {
	// real dir + a symlink pointing at it; root is defined via the symlink.
	real := filepath.Join(t.TempDir(), "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	// absolute input pointing at the symlink target must resolve as inside.
	abs := filepath.Join(real, "new.txt")
	_, rel, _, ok := ResolveInRoot(link, abs)
	if !ok {
		t.Fatalf("symlink-target path rejected, want inside; abs=%s", abs)
	}
	if rel != "new.txt" {
		t.Errorf("rel=%q want new.txt", rel)
	}
}

func TestResolveInRoot_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	_, rel, _, ok := ResolveInRoot(home, "~/foo.txt")
	if !ok {
		t.Fatal("~/foo.txt under home root rejected")
	}
	if rel != "foo.txt" {
		t.Errorf("rel=%q want foo.txt", rel)
	}
}

func TestResolveInRoot_Escape(t *testing.T) {
	root := t.TempDir()
	if _, _, _, ok := ResolveInRoot(root, "../escape.txt"); ok {
		t.Fatal("../escape.txt accepted, want rejected")
	}
}
