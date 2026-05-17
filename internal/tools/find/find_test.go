package find

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFind_BasicGlob(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(dir)
	res, err := f.Run(context.Background(), map[string]any{"name": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "a.go") || strings.Contains(res.Content, "b.txt") {
		t.Errorf("want a.go only, got: %s", res.Content)
	}
}

func TestFind_MissingName(t *testing.T) {
	f := New(t.TempDir())
	res, _ := f.Run(context.Background(), map[string]any{})
	if !res.IsError {
		t.Errorf("want IsError when pattern missing")
	}
}

func TestFind_PatternAlias(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New(dir).Run(context.Background(), map[string]any{"pattern": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "a.go") {
		t.Errorf("pattern arg should match like name; got: %s", res.Content)
	}
}

func TestFind_DoubleStarPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub", "nest"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "nest", "Bunker.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := New(dir).Run(context.Background(), map[string]any{"pattern": "**/Bunker*.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "Bunker.ts") {
		t.Errorf("**/ prefix should be stripped and recurse; got: %s", res.Content)
	}
}

func TestFind_BadPattern(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(dir)
	res, _ := f.Run(context.Background(), map[string]any{"name": "[bad"})
	if !res.IsError {
		t.Errorf("want IsError for bad pattern")
	}
}

func TestFind_SkipDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "x.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "y.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(dir)
	res, _ := f.Run(context.Background(), map[string]any{"name": "*.go"})
	if strings.Contains(res.Content, "node_modules") {
		t.Errorf("node_modules should be skipped, got: %s", res.Content)
	}
}

func TestFind_RelativePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b.go"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{"name": "*.go"})
	if strings.Contains(res.Content, dir) {
		t.Fatalf("emitted absolute path %q: %s", dir, res.Content)
	}
	if !strings.Contains(res.Content, filepath.Join("a", "b.go")) {
		t.Fatalf("expected relative a/b.go, got: %s", res.Content)
	}
}

func TestFind_NoMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := New(dir)
	res, err := f.Run(context.Background(), map[string]any{"name": "*.go"})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "no matches") {
		t.Errorf("want 'no matches', got: %s", res.Content)
	}
}
