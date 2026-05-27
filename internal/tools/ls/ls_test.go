package ls

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLS_FilesAndDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	l := New(dir)
	res, err := l.Run(context.Background(), map[string]any{"path": dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %s", len(lines), res.Content)
	}
	gotDir := false
	gotFile := false
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			t.Fatalf("malformed line: %q", line)
		}
		if parts[0] == "d" && parts[2] == "sub" {
			gotDir = true
		}
		if parts[0] == "f" && parts[2] == "a.txt" {
			gotFile = true
		}
	}
	if !gotDir || !gotFile {
		t.Errorf("missing entries; got: %s", res.Content)
	}
}

func TestLS_DefaultsToRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "z.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(dir)
	res, err := l.Run(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "z.txt") {
		t.Errorf("want z.txt, got: %s", res.Content)
	}
}

func TestLS_Sorted(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"c.txt", "a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(dir)
	res, _ := l.Run(context.Background(), map[string]any{"path": dir})
	lines := strings.Split(strings.TrimRight(res.Content, "\n"), "\n")
	var names []string
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) == 3 {
			names = append(names, parts[2])
		}
	}
	if len(names) != 3 || names[0] != "a.txt" || names[1] != "b.txt" || names[2] != "c.txt" {
		t.Errorf("want sorted a,b,c, got: %v", names)
	}
}

func TestLS_BadPath(t *testing.T) {
	l := New(t.TempDir())
	res, _ := l.Run(context.Background(), map[string]any{"path": "/no/such/path/xyz123"})
	if !res.IsError {
		t.Errorf("want IsError for bad path")
	}
}

func TestLS_TraversalRejected(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	res, _ := l.Run(context.Background(), map[string]any{"path": "../../../etc"})
	if !res.IsError {
		t.Errorf("want IsError for traversal, got: %s", res.Content)
	}
}

func TestLS_AbsoluteOutsideRootRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	l := New(root)
	res, _ := l.Run(context.Background(), map[string]any{"path": outside})
	if !res.IsError {
		t.Errorf("want IsError for absolute path outside root, got: %s", res.Content)
	}
}

func TestLS_RelativePathJoinedToRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "inner.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(dir)
	res, err := l.Run(context.Background(), map[string]any{"path": "sub"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("err: %s", res.Content)
	}
	if !strings.Contains(res.Content, "inner.txt") {
		t.Errorf("want inner.txt, got: %s", res.Content)
	}
}

func TestLS_SymlinkMarkedL(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	l := New(dir)
	res, _ := l.Run(context.Background(), map[string]any{"path": dir})
	lines := strings.Split(res.Content, "\n")
	found := false
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) == 3 && parts[2] == "link" {
			if parts[0] != "l" {
				t.Errorf("want kind 'l' for symlink, got %q in %q", parts[0], line)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("symlink not listed; got: %s", res.Content)
	}
}

func TestLS_Truncated(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 505; i++ {
		name := fmt.Sprintf("f%04d.txt", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := New(dir)
	res, _ := l.Run(context.Background(), map[string]any{"path": dir})
	if !strings.Contains(res.Content, "(truncated; 5 more)") {
		t.Errorf("want truncation marker for 505 entries")
	}
	lines := strings.Split(res.Content, "\n")
	// 500 entries + 1 truncation line
	if len(lines) != 501 {
		t.Errorf("want 501 lines, got %d", len(lines))
	}
}

func TestLS_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	res, _ := l.Run(context.Background(), map[string]any{"path": dir})
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if res.Content != "empty directory" {
		t.Errorf("want 'empty directory', got: %q", res.Content)
	}
}

// regression: ls escape error must echo offending path + workspace root so
// model can self-correct (matches write tool behavior — see write_test.go
// TestWrite_EscapeEchoesPathAndRoot).
func TestLS_EscapeEchoesPathAndRoot(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	res, _ := l.Run(context.Background(), map[string]any{"path": "/tmp"})
	if !res.IsError {
		t.Fatal("want IsError for escape path")
	}
	if !strings.Contains(res.Content, "/tmp") {
		t.Errorf("error must echo offending path; got: %s", res.Content)
	}
	if !strings.Contains(res.Content, dir) {
		t.Errorf("error must echo workspace root %q; got: %s", dir, res.Content)
	}
	if !strings.Contains(res.Content, "relative to") || !strings.Contains(res.Content, "workspace root") {
		t.Errorf("error must hint at fix; got: %s", res.Content)
	}
}
