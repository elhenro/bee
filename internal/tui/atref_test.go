package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCompletionCandidates(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "internal"), 0o755)
	os.MkdirAll(filepath.Join(dir, "intent"), 0o755)
	os.WriteFile(filepath.Join(dir, "other.txt"), nil, 0o644)
	got := CompletionCandidates(dir, "int")
	if len(got) != 2 {
		t.Fatalf("want 2 matches, got %v", got)
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{"internal", "intent"}, "inte"},
		{[]string{"foo", "bar"}, ""},
		{[]string{"only"}, "only"},
		{[]string{}, ""},
	}
	for _, tt := range tests {
		if got := LongestCommonPrefix(tt.in); got != tt.want {
			t.Errorf("LongestCommonPrefix(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCompletePath(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "only.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "alpha.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "alpha_test.go"), nil, 0o644)
	tests := []struct {
		val, want string
		ok        bool
	}{
		{"see al", "see alpha", true},
		{"alpha", "alpha", false},
		{"sub/", "sub/only.txt", true}, // trailing slash lists the directory
		{"sub/on", "sub/only.txt", true},
		{"./su", "./sub", true},
		{"nope", "nope", false},
		{"say ", "say ", false},
	}
	for _, tt := range tests {
		got, ok := completePath(dir, tt.val)
		if got != tt.want || ok != tt.ok {
			t.Errorf("completePath(%q) = %q, %v; want %q, %v", tt.val, got, ok, tt.want, tt.ok)
		}
	}
}

func TestIndexFiles_BasicMatch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "internal", "tui"), 0o755)
	os.WriteFile(filepath.Join(dir, "internal", "tui", "app.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), nil, 0o644)
	got := indexFiles(dir).filter("app")
	found := false
	for _, p := range got {
		if filepath.Base(p) == "app.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("filter didn't return app.go, got %v", got)
	}
}

func TestIndexFiles_SkipsDotGit(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "normal.txt"), nil, 0o644)
	got := indexFiles(dir).filter("")
	for _, p := range got {
		if filepath.Base(p) == "config" {
			t.Errorf("should not include .git files, got %v", got)
		}
	}
}

func TestIndexFiles_SkipsHiddenDotDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude", "worktrees", "feat"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude", "worktrees", "feat", "leak.go"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, ".idea"), 0o755)
	os.WriteFile(filepath.Join(dir, ".idea", "ide.xml"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "keep.go"), nil, 0o644)
	got := indexFiles(dir).filter("")
	for _, p := range got {
		b := filepath.Base(p)
		if b == "leak.go" || b == "ide.xml" {
			t.Errorf("should skip hidden dotfolders, got %v", got)
		}
	}
}

func TestIndexFiles_SkipsWorktrees(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "worktrees", "wt1"), 0o755)
	os.WriteFile(filepath.Join(dir, "worktrees", "wt1", "x.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), nil, 0o644)
	got := indexFiles(dir).filter("")
	for _, p := range got {
		if filepath.Base(p) == "x.go" {
			t.Errorf("should skip worktrees dir, got %v", got)
		}
	}
}

func TestIndexFiles_SkipsBuildAndVendorDirs(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"target", "dist", "build", "node_modules", "__pycache__", "src"} {
		os.MkdirAll(filepath.Join(dir, d), 0o755)
		os.WriteFile(filepath.Join(dir, d, "f.go"), nil, 0o644)
	}
	got := indexFiles(dir).filter("")
	if len(got) != 1 || got[0] != filepath.Join("src", "f.go") {
		t.Errorf("want only src/f.go, got %v", got)
	}
}

func TestIndexFilter_CaseInsensitiveAndCapped(t *testing.T) {
	dir := t.TempDir()
	for i := range fuzzyMax + 10 {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("File%03d.go", i)), nil, 0o644)
	}
	idx := indexFiles(dir)
	if got := idx.filter("file0"); len(got) != fuzzyMax {
		t.Errorf("want %d capped matches, got %d", fuzzyMax, len(got))
	}
	if got := idx.filter("FILE001"); len(got) != 1 {
		t.Errorf("case-insensitive match failed: %v", got)
	}
	if got := (*fileIndex)(nil).filter("x"); got != nil {
		t.Errorf("nil index must match nothing, got %v", got)
	}
}
