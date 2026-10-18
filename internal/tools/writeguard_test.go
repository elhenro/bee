package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// a dangling symlink as the final (or an intermediate) component would let the
// first create land outside the root; confined resolution must refuse it.
func TestResolveInRoot_DanglingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "gone"), filepath.Join(root, "evil")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"evil", "evil/sub/f.txt"} {
		if _, _, _, ok := ResolveInRoot(root, p); ok {
			t.Errorf("dangling symlink path %q accepted", p)
		}
	}
	if _, _, _, ok := ResolveInRoot(root, "fresh/new.txt"); !ok {
		t.Error("plain missing path rejected")
	}
}

func TestFilterPath(t *testing.T) {
	root := t.TempDir()
	rel, ok := FilterPath(root, filepath.Join(root, "docs", "..", "docs", "a.md"))
	if !ok || rel != "docs/a.md" {
		t.Errorf("got %q %v", rel, ok)
	}
	if _, ok := FilterPath(root, filepath.Join(root, "..", "x")); ok {
		t.Error("path outside root accepted")
	}
}

func TestCheckWriteTarget(t *testing.T) {
	root := t.TempDir()
	re := regexp.MustCompile(`^docs/`)
	if msg := CheckWriteTarget(root, filepath.Join(root, "docs", "a.md"), re); msg != "" {
		t.Errorf("allowed path denied: %s", msg)
	}
	if msg := CheckWriteTarget(root, filepath.Join(root, "docs", "..", "x.md"), re); msg == "" {
		t.Error("dot-dot bypass of filter accepted")
	}
	if msg := CheckWriteTarget(root, filepath.Join(root, ".git", "hooks", "pre-commit"), nil); msg == "" {
		t.Error("confined write into .git accepted")
	}
	// danger scope (empty root): no sensitive-path check, filter is cwd-relative
	if msg := CheckWriteTarget("", filepath.Join(t.TempDir(), ".git", "hooks", "x"), nil); msg != "" {
		t.Errorf("danger scope denied: %s", msg)
	}
	wd, _ := os.Getwd()
	if msg := CheckWriteTarget("", filepath.Join(wd, "docs", "a.md"), re); msg != "" {
		t.Errorf("danger scope filter denied cwd-relative path: %s", msg)
	}
}
