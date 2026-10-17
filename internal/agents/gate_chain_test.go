//go:build !windows

package agents

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func link(t *testing.T, target, at string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(at), 0o755)
	if err := os.Symlink(target, at); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, p string) {
	t.Helper()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLinkChainCoversEveryHop(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	touch(t, filepath.Join(root, "cfg", "real"))
	link(t, "../a", filepath.Join(root, ".bee", "verify"))
	link(t, "b", filepath.Join(root, "a"))
	link(t, "cfg/real", filepath.Join(root, "b"))
	got := linkChain(root, ".bee/verify")
	want := []string{".bee/verify", "a", "b", "cfg/real"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestLinkChainSymlinkedParentDir(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	touch(t, filepath.Join(root, "cfg", "verify"))
	link(t, "cfg", filepath.Join(root, ".bee"))
	if got := linkChain(root, ".bee/verify"); !slices.Equal(got, []string{".bee", "cfg/verify"}) {
		t.Fatalf("got %v", got)
	}
}

func TestLinkChainStopsOutsideRootAndOnLoops(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	outside := filepath.Join(t.TempDir(), "verify")
	touch(t, outside)
	link(t, outside, filepath.Join(root, ".bee", "verify"))
	if got := linkChain(root, ".bee/verify"); !slices.Equal(got, []string{".bee/verify"}) {
		t.Fatalf("outside target: got %v", got)
	}
	loop := t.TempDir()
	link(t, "y", filepath.Join(loop, "x"))
	link(t, "x", filepath.Join(loop, "y"))
	if got := linkChain(loop, "x"); len(got) != maxLinkHops {
		t.Fatalf("loop must stop at the hop cap, got %d entries", len(got))
	}
}
