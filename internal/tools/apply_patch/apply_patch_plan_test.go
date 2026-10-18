package apply_patch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// two sections for one file must apply in order, not both against the original.
func TestApplyPatch_TwoSectionsSameFile(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "f.txt")
	if err := os.WriteFile(f, []byte("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := "--- a/f.txt\n+++ b/f.txt\n@@ -1,2 +1,2 @@\n-a\n+A\n b\n--- a/f.txt\n+++ b/f.txt\n@@ -9,2 +9,2 @@\n i\n-j\n+J\n"
	res, _ := New(root).Run(context.Background(), map[string]any{"patch": patch})
	if res.IsError {
		t.Fatalf("patch failed: %s", res.Content)
	}
	b, _ := os.ReadFile(f)
	if string(b) != "A\nb\nc\nd\ne\nf\ng\nh\ni\nJ\n" {
		t.Errorf("both sections must land, got %q", b)
	}
}

func TestApplyPatch_Rename(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("one\ntwo\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	patch := "diff --git a/old.txt b/new.txt\nsimilarity index 80%\nrename from old.txt\nrename to new.txt\n--- a/old.txt\n+++ b/new.txt\n@@ -1,2 +1,2 @@\n one\n-two\n+TWO\n"
	res, _ := New(root).Run(context.Background(), map[string]any{"patch": patch})
	if res.IsError {
		t.Fatalf("rename failed: %s", res.Content)
	}
	b, err := os.ReadFile(filepath.Join(root, "new.txt"))
	if err != nil || string(b) != "one\nTWO\n" {
		t.Errorf("new.txt = %q, %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(root, "old.txt")); err == nil {
		t.Error("old.txt still present after rename")
	}
}
