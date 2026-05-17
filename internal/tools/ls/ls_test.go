package ls

import (
	"context"
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
