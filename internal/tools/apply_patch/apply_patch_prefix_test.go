package apply_patch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(rel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rel, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGitHeaderPatchOnTopLevelDirA(t *testing.T) {
	dir := runInDir(t)
	writeFile(t, "a/x.txt", "one\ntwo\nthree\n")
	writeFile(t, "x.txt", "one\ntwo\nthree\n")
	patch := "diff --git a/a/x.txt b/a/x.txt\n--- a/a/x.txt\n+++ b/a/x.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	res, err := New(dir).Run(context.Background(), map[string]any{"patch": patch})
	if err != nil || res.IsError {
		t.Fatalf("apply failed: %v %s", err, res.Content)
	}
	if got, _ := os.ReadFile("a/x.txt"); string(got) != "one\nTWO\nthree\n" {
		t.Errorf("a/x.txt not patched: %q", got)
	}
	if got, _ := os.ReadFile("x.txt"); string(got) != "one\ntwo\nthree\n" {
		t.Errorf("x.txt (wrong file) was modified: %q", got)
	}
}

func TestBareDiffStillStripsPrefix(t *testing.T) {
	dir := runInDir(t)
	writeFile(t, "x.txt", "one\ntwo\nthree\n")
	patch := "--- a/x.txt\n+++ b/x.txt\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n"
	res, err := New(dir).Run(context.Background(), map[string]any{"patch": patch})
	if err != nil || res.IsError {
		t.Fatalf("apply failed: %v %s", err, res.Content)
	}
	if got, _ := os.ReadFile("x.txt"); string(got) != "one\nTWO\nthree\n" {
		t.Errorf("x.txt not patched: %q", got)
	}
}
