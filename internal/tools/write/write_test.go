package write

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestWrite_Basic(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	res, err := w.Run(context.Background(), map[string]any{
		"path":    filepath.Join(dir, "a.txt"),
		"content": "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	data, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("want 'hello', got %q", string(data))
	}
}

func TestWrite_RelativePath(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	res, err := w.Run(context.Background(), map[string]any{
		"path":    "sub/a.txt",
		"content": "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sub", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi" {
		t.Errorf("want 'hi', got %q", string(data))
	}
}

func TestWrite_CreatesParents(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{
		"path":    filepath.Join(dir, "a/b/c.txt"),
		"content": "x",
	})
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c.txt")); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestWrite_Overwrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{"path": p, "content": "new"})
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "new" {
		t.Errorf("want 'new', got %q", string(data))
	}
}

func TestWrite_Escape(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{
		"path":    "../escape.txt",
		"content": "bad",
	})
	if !res.IsError {
		t.Errorf("want IsError for escape path")
	}
	if !strings.Contains(res.Content, "escapes workspace root") {
		t.Errorf("want 'escapes workspace root' msg, got: %s", res.Content)
	}
}

func TestWrite_MissingPath(t *testing.T) {
	w := New(t.TempDir())
	res, _ := w.Run(context.Background(), map[string]any{"content": "x"})
	if !res.IsError {
		t.Errorf("want IsError for missing path")
	}
}

func TestWrite_MissingContent(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{"path": filepath.Join(dir, "a.txt")})
	if !res.IsError {
		t.Errorf("want IsError for missing content")
	}
}

func TestWrite_FilterNilAllowsAll(t *testing.T) {
	dir := t.TempDir()
	w := NewWithFilter(dir, nil)
	res, _ := w.Run(context.Background(), map[string]any{
		"path": "a.go", "content": "package x",
	})
	if res.IsError {
		t.Fatalf("nil filter must allow all paths: %s", res.Content)
	}
}

func TestWrite_FilterAllowsMatch(t *testing.T) {
	dir := t.TempDir()
	w := NewWithFilter(dir, regexp.MustCompile(`\.md$`))
	res, _ := w.Run(context.Background(), map[string]any{
		"path": "foo.md", "content": "# hi",
	})
	if res.IsError {
		t.Fatalf("md path must pass filter: %s", res.Content)
	}
	data, err := os.ReadFile(filepath.Join(dir, "foo.md"))
	if err != nil || string(data) != "# hi" {
		t.Fatalf("file not written: %v %q", err, data)
	}
}

func TestWrite_FilterRejectsMiss(t *testing.T) {
	dir := t.TempDir()
	w := NewWithFilter(dir, regexp.MustCompile(`\.md$`))
	res, _ := w.Run(context.Background(), map[string]any{
		"path": "foo.go", "content": "package x",
	})
	if !res.IsError {
		t.Fatalf("want IsError for non-md path")
	}
	if !strings.Contains(res.Content, "denied") {
		t.Errorf("want 'denied' in msg, got: %s", res.Content)
	}
	if _, err := os.Stat(filepath.Join(dir, "foo.go")); !os.IsNotExist(err) {
		t.Errorf("file must not be created on rejection: %v", err)
	}
}

// regression: small models occasionally drop a unified diff into write.content
// (see ~/web/bee fix-cost-monitor incident). guard must refuse and leave the
// target untouched, redirecting the model to edit/apply_patch.
func TestWrite_RejectsUnifiedDiff(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "tui.go")
	if err := os.WriteFile(target, []byte("original content"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := New(dir)
	diff := "--- a/tui.go\n+++ b/tui.go\n@@ -1,3 +1,3 @@\n-old\n+new\n"
	res, _ := w.Run(context.Background(), map[string]any{"path": target, "content": diff})
	if !res.IsError {
		t.Fatalf("want IsError for diff content, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "edit") || !strings.Contains(res.Content, "diff") {
		t.Errorf("error msg should redirect to edit/apply_patch; got: %s", res.Content)
	}
	// file must be UNTOUCHED — that's the whole point.
	data, _ := os.ReadFile(target)
	if string(data) != "original content" {
		t.Fatalf("guard let the file get overwritten: %q", string(data))
	}
}

func TestWrite_RejectsHunkHeaderOnly(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{
		"path":    "x.go",
		"content": "@@ -10,5 +10,5 @@\n some\n+new\n-old\n context\n",
	})
	if !res.IsError {
		t.Fatal("hunk header alone should still be rejected")
	}
}

func TestWrite_RejectsCodexEnvelope(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{
		"path":    "x.go",
		"content": "*** Begin Patch\n*** Update File: x.go\n@@\n-old\n+new\n*** End Patch\n",
	})
	if !res.IsError {
		t.Fatal("codex patch envelope should be rejected")
	}
}

func TestWrite_AllowsYAMLFrontmatter(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	// `---` alone (no `+++`) is YAML frontmatter, NOT a diff. Must pass.
	yaml := "---\nname: foo\ntype: prompt\n---\nbody\n"
	res, _ := w.Run(context.Background(), map[string]any{"path": "x.md", "content": yaml})
	if res.IsError {
		t.Fatalf("YAML frontmatter must not be misclassified: %s", res.Content)
	}
}

// empty string content must be refused; silently writing a zero-byte file
// looks like an accidental file-delete and confuses callers.
func TestWrite_RejectsEmptyContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{"path": p, "content": ""})
	if !res.IsError {
		t.Fatal("want IsError for empty content")
	}
	if !strings.Contains(res.Content, "non-empty") {
		t.Errorf("want 'non-empty' in msg, got: %s", res.Content)
	}
	data, _ := os.ReadFile(p)
	if string(data) != "keep me" {
		t.Errorf("existing file must be untouched, got: %q", string(data))
	}
}

// overwriting an executable file must preserve the 0o755 bit so the binary
// stays runnable; hardcoded 0o644 would silently strip it.
func TestWrite_PreservesExecutableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows has no executable bit")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "run.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{
		"path": p, "content": "#!/bin/sh\necho new\n",
	})
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("want mode 0o755 preserved, got %o", info.Mode().Perm())
	}
}

func TestWrite_AllowsNormalCode(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	res, _ := w.Run(context.Background(), map[string]any{
		"path":    "x.go",
		"content": "package x\n\nfunc Foo() {}\n",
	})
	if res.IsError {
		t.Fatalf("normal code must not be misclassified: %s", res.Content)
	}
}
