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

// rooted-but-volume-less inputs must be treated as absolute, not joined under
// root. on Windows filepath.IsAbs("/tmp") is false, so without isRooted these
// would resolve to root\tmp and falsely pass containment — the long-standing
// Windows CI failure on TestLS/Write_EscapeEchoesPathAndRoot.
func TestIsRooted(t *testing.T) {
	for _, p := range []string{"/tmp", "\\tmp", "/", "\\"} {
		if !isRooted(p) {
			t.Errorf("isRooted(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"tmp", "a/b", "", "C:foo"} {
		if isRooted(p) {
			t.Errorf("isRooted(%q) = true, want false", p)
		}
	}
}

// pathIsPrefix: the root contains itself (regression — listing the workspace
// root must not report "escapes root"), but a sibling sharing a name prefix
// must not be treated as contained.
func TestPathIsPrefix_RootContainsSelfButNotSibling(t *testing.T) {
	if !pathIsPrefix("/x/001", "/x/001") {
		t.Error("root must contain itself")
	}
	if pathIsPrefix("/x/0011", "/x/001") {
		t.Error("/x/0011 is a sibling, not inside /x/001")
	}
	if !pathIsPrefix("/x/001/sub", "/x/001") {
		t.Error("/x/001/sub must be inside /x/001")
	}
}
