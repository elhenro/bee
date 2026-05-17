package apply_patch

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runInDir cds the test into a tmpdir for the duration.
func runInDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	return dir
}

func TestCreateNewFile(t *testing.T) {
	runInDir(t)
	patch := `diff --git a/hello.txt b/hello.txt
new file mode 100644
--- /dev/null
+++ b/hello.txt
@@ -0,0 +1,2 @@
+hello
+world
`
	tool := New()
	res, err := tool.Run(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	got, err := os.ReadFile("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\nworld\n" {
		t.Fatalf("contents mismatch: %q", got)
	}
	if !strings.Contains(res.Content, "+ hello.txt") {
		t.Fatalf("summary missing create marker: %s", res.Content)
	}
}

func TestModifyExistingFile(t *testing.T) {
	runInDir(t)
	if err := os.WriteFile("a.txt", []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,3 +1,3 @@
 one
-two
+TWO
 three
`
	res, err := New().Run(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	got, _ := os.ReadFile("a.txt")
	if string(got) != "one\nTWO\nthree\n" {
		t.Fatalf("contents mismatch: %q", got)
	}
	if !strings.Contains(res.Content, "~ a.txt") {
		t.Fatalf("summary missing modify marker: %s", res.Content)
	}
}

func TestMultiFilePatch(t *testing.T) {
	runInDir(t)
	if err := os.WriteFile("x.txt", []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := `diff --git a/x.txt b/x.txt
--- a/x.txt
+++ b/x.txt
@@ -1 +1 @@
-x
+X
diff --git a/y.txt b/y.txt
new file mode 100644
--- /dev/null
+++ b/y.txt
@@ -0,0 +1 @@
+y
`
	res, err := New().Run(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	x, _ := os.ReadFile("x.txt")
	y, _ := os.ReadFile("y.txt")
	if string(x) != "X\n" {
		t.Fatalf("x mismatch: %q", x)
	}
	if string(y) != "y\n" {
		t.Fatalf("y mismatch: %q", y)
	}
	if !strings.Contains(res.Content, "applied 2 file(s)") {
		t.Fatalf("summary count wrong: %s", res.Content)
	}
}

func TestContextMismatchFails(t *testing.T) {
	runInDir(t)
	if err := os.WriteFile("a.txt", []byte("actual\nother\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// patch claims line 1 is "expected" but file has "actual"
	patch := `diff --git a/a.txt b/a.txt
--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,2 @@
-expected
+changed
 other
`
	res, err := New().Run(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("expected conflict error, got success: %s", res.Content)
	}
	// confirm file untouched
	got, _ := os.ReadFile("a.txt")
	if string(got) != "actual\nother\n" {
		t.Fatalf("file mutated on conflict: %q", got)
	}
}

func TestDeleteFile(t *testing.T) {
	runInDir(t)
	if err := os.WriteFile("gone.txt", []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := `diff --git a/gone.txt b/gone.txt
deleted file mode 100644
--- a/gone.txt
+++ /dev/null
@@ -1,2 +0,0 @@
-a
-b
`
	res, err := New().Run(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if _, err := os.Stat("gone.txt"); !os.IsNotExist(err) {
		t.Fatalf("file should be gone, stat err=%v", err)
	}
	if !strings.Contains(res.Content, "- gone.txt") {
		t.Fatalf("summary missing delete marker: %s", res.Content)
	}
}

func TestEmptyPatchRejected(t *testing.T) {
	res, _ := New().Run(context.Background(), map[string]any{"patch": ""})
	if !res.IsError {
		t.Fatal("expected error for empty patch")
	}
}

func TestCreateInSubdir(t *testing.T) {
	runInDir(t)
	patch := `diff --git a/sub/dir/n.txt b/sub/dir/n.txt
new file mode 100644
--- /dev/null
+++ b/sub/dir/n.txt
@@ -0,0 +1 @@
+nested
`
	res, err := New().Run(context.Background(), map[string]any{"patch": patch})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	got, err := os.ReadFile(filepath.Join("sub", "dir", "n.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "nested\n" {
		t.Fatalf("contents mismatch: %q", got)
	}
}

func TestSpec(t *testing.T) {
	s := New().Spec()
	if s.Name != "apply_patch" {
		t.Fatalf("wrong name: %s", s.Name)
	}
	if s.Schema == nil {
		t.Fatal("nil schema")
	}
}

func TestApplyPatch_FilterNilAllowsAll(t *testing.T) {
	runInDir(t)
	patch := `diff --git a/foo.go b/foo.go
new file mode 100644
--- /dev/null
+++ b/foo.go
@@ -0,0 +1 @@
+package x
`
	tool := NewWithFilter(nil)
	res, _ := tool.Run(context.Background(), map[string]any{"patch": patch})
	if res.IsError {
		t.Fatalf("nil filter must allow: %s", res.Content)
	}
	if _, err := os.Stat("foo.go"); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestApplyPatch_FilterAllowsMatch(t *testing.T) {
	runInDir(t)
	patch := `diff --git a/notes.md b/notes.md
new file mode 100644
--- /dev/null
+++ b/notes.md
@@ -0,0 +1 @@
+hi
`
	tool := NewWithFilter(regexp.MustCompile(`\.md$`))
	res, _ := tool.Run(context.Background(), map[string]any{"patch": patch})
	if res.IsError {
		t.Fatalf("md path must pass: %s", res.Content)
	}
}

func TestApplyPatch_FilterRejectsMiss(t *testing.T) {
	runInDir(t)
	patch := `diff --git a/foo.go b/foo.go
new file mode 100644
--- /dev/null
+++ b/foo.go
@@ -0,0 +1 @@
+package x
`
	tool := NewWithFilter(regexp.MustCompile(`\.md$`))
	res, _ := tool.Run(context.Background(), map[string]any{"patch": patch})
	if !res.IsError {
		t.Fatalf("want IsError for non-md path")
	}
	if !strings.Contains(res.Content, "denied") {
		t.Errorf("want 'denied' in msg, got: %s", res.Content)
	}
	if _, err := os.Stat("foo.go"); !os.IsNotExist(err) {
		t.Errorf("file must not be created on rejection: %v", err)
	}
}

func TestApplyPatch_RepairsHunkCount(t *testing.T) {
	runInDir(t)
	if err := os.WriteFile("a.txt", []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// intentionally wrong @@ -1,3 +1,3 @@ (correct would be -1,3 +1,4)
	bad := "--- a/a.txt\n+++ b/a.txt\n@@ -1,3 +1,3 @@\n a\n+X\n b\n c\n"
	r, err := New().Run(context.Background(), map[string]any{"patch": bad})
	if err != nil || r.IsError {
		t.Fatalf("repair failed: %v %s", err, r.Content)
	}
	got, _ := os.ReadFile("a.txt")
	if string(got) != "a\nX\nb\nc\n" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyPatch_StripsAPrefix(t *testing.T) {
	runInDir(t)
	if err := os.WriteFile("hello.go", []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// bare diff with a/ b/ prefixes but no "diff --git" header
	patch := "--- a/hello.go\n+++ b/hello.go\n@@ -1 +1 @@\n-hi\n+bye\n"
	r, err := New().Run(context.Background(), map[string]any{"patch": patch})
	if err != nil || r.IsError {
		t.Fatalf("apply: %v %s", err, r.Content)
	}
	got, _ := os.ReadFile("hello.go")
	if string(got) != "bye\n" {
		t.Fatalf("want bye, got %q", got)
	}
}

// when any file in a multi-file patch fails the filter, the entire batch
// is rejected before any write happens.
func TestApplyPatch_FilterRejectsBatchAtomically(t *testing.T) {
	runInDir(t)
	patch := `diff --git a/ok.md b/ok.md
new file mode 100644
--- /dev/null
+++ b/ok.md
@@ -0,0 +1 @@
+ok
diff --git a/bad.go b/bad.go
new file mode 100644
--- /dev/null
+++ b/bad.go
@@ -0,0 +1 @@
+nope
`
	tool := NewWithFilter(regexp.MustCompile(`\.md$`))
	res, _ := tool.Run(context.Background(), map[string]any{"patch": patch})
	if !res.IsError {
		t.Fatalf("want IsError for mixed batch")
	}
	if _, err := os.Stat("ok.md"); !os.IsNotExist(err) {
		t.Errorf("ok.md must NOT exist; full batch should be rejected: %v", err)
	}
	if _, err := os.Stat("bad.go"); !os.IsNotExist(err) {
		t.Errorf("bad.go must NOT exist: %v", err)
	}
	_ = filepath.Separator
}
