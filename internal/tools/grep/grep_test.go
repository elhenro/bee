package grep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrep_BasicMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nfunc Foo(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("package b\nfunc Bar(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := New(dir)
	res, err := g.Run(context.Background(), map[string]any{"pattern": "Foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "a.go") || strings.Contains(res.Content, "b.go") {
		t.Errorf("want a.go only, got: %s", res.Content)
	}
}

func TestGrep_GlobFilter(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("Foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("Foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := New(dir)
	res, _ := g.Run(context.Background(), map[string]any{"pattern": "Foo", "glob": "go"})
	if !strings.Contains(res.Content, "a.go") || strings.Contains(res.Content, "b.txt") {
		t.Errorf("glob filter failed, got: %s", res.Content)
	}
}

func TestGrep_BadRegex(t *testing.T) {
	g := New(t.TempDir())
	res, _ := g.Run(context.Background(), map[string]any{"pattern": "[invalid"})
	if !res.IsError {
		t.Errorf("want IsError for bad regex")
	}
}

func TestGrep_MissingPattern(t *testing.T) {
	g := New(t.TempDir())
	res, _ := g.Run(context.Background(), map[string]any{})
	if !res.IsError {
		t.Errorf("want IsError when pattern missing")
	}
}

func TestGrep_NoMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := New(dir)
	res, err := g.Run(context.Background(), map[string]any{"pattern": "Foo"})
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

func TestGrep_SkipBinary(t *testing.T) {
	dir := t.TempDir()
	// binary: NUL byte in first chunk + the literal pattern; should be skipped
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte("Foo\x00bar"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src.go"), []byte("Foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := New(dir)
	res, _ := g.Run(context.Background(), map[string]any{"pattern": "Foo"})
	if strings.Contains(res.Content, "blob.bin") {
		t.Errorf("binary file should be skipped, got: %s", res.Content)
	}
	if !strings.Contains(res.Content, "src.go") {
		t.Errorf("want src.go match, got: %s", res.Content)
	}
}

func TestGrep_ContextLines(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("one\ntwo\nFoo\nfour\nfive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{"pattern": "Foo", "context": 1})
	// match line uses ':' separator, context lines use '-'
	if !strings.Contains(res.Content, "a.go:2-two") {
		t.Fatalf("missing pre-context line:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "a.go:3:Foo") {
		t.Fatalf("missing match line:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "a.go:4-four") {
		t.Fatalf("missing post-context line:\n%s", res.Content)
	}
}

func TestGrep_ContextDedupAdjacent(t *testing.T) {
	dir := t.TempDir()
	// two adjacent matches with context=2 — overlapping windows must not
	// emit duplicate lines
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("x\ny\nFoo\nFoo\nz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{"pattern": "Foo", "context": 2})
	// count number of "y" emissions — should be exactly 1 despite two windows
	if strings.Count(res.Content, ":2-y") > 1 {
		t.Fatalf("duplicate context line emitted:\n%s", res.Content)
	}
}

func TestGrep_CountOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("Foo\nFoo\nbar\nFoo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.go"), []byte("Foo\nzz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{"pattern": "Foo", "count_only": true})
	if !strings.Contains(res.Content, "a.go:3") {
		t.Fatalf("missing a.go:3 count:\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "b.go:1") {
		t.Fatalf("missing b.go:1 count:\n%s", res.Content)
	}
	// body lines must not leak
	if strings.Contains(res.Content, "Foo\n") {
		t.Fatalf("count_only leaked match bodies:\n%s", res.Content)
	}
}

func TestGrep_RelativePaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b.go"), []byte("Foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := New(dir).Run(context.Background(), map[string]any{"pattern": "Foo"})
	if strings.Contains(res.Content, dir) {
		t.Fatalf("emitted absolute path %q: %s", dir, res.Content)
	}
	if !strings.Contains(res.Content, filepath.Join("a", "b.go")+":1:") {
		t.Fatalf("expected relative a/b.go:1:, got: %s", res.Content)
	}
}

func TestGrep_SkipDirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "hit.go"), []byte("Foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("Foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := New(dir)
	res, _ := g.Run(context.Background(), map[string]any{"pattern": "Foo"})
	if strings.Contains(res.Content, ".git") {
		t.Errorf(".git should be skipped, got: %s", res.Content)
	}
}
