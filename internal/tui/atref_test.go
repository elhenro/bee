package tui

import (
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

func TestFuzzyFiles_BasicMatch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "internal", "tui"), 0o755)
	os.WriteFile(filepath.Join(dir, "internal", "tui", "app.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "README.md"), nil, 0o644)
	got := FuzzyFiles(dir, "app")
	found := false
	for _, p := range got {
		if filepath.Base(p) == "app.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("FuzzyFiles didn't return app.go, got %v", got)
	}
}

func TestFuzzyFiles_SkipsDotGit(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "normal.txt"), nil, 0o644)
	got := FuzzyFiles(dir, "")
	for _, p := range got {
		if filepath.Base(p) == "config" {
			t.Errorf("should not include .git files, got %v", got)
		}
	}
}

func TestFuzzyFiles_SkipsHiddenDotDirs(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".claude", "worktrees", "feat"), 0o755)
	os.WriteFile(filepath.Join(dir, ".claude", "worktrees", "feat", "leak.go"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, ".idea"), 0o755)
	os.WriteFile(filepath.Join(dir, ".idea", "ide.xml"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "keep.go"), nil, 0o644)
	got := FuzzyFiles(dir, "")
	for _, p := range got {
		b := filepath.Base(p)
		if b == "leak.go" || b == "ide.xml" {
			t.Errorf("should skip hidden dotfolders, got %v", got)
		}
	}
}

func TestFuzzyFiles_SkipsWorktrees(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "worktrees", "wt1"), 0o755)
	os.WriteFile(filepath.Join(dir, "worktrees", "wt1", "x.go"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), nil, 0o644)
	got := FuzzyFiles(dir, "")
	for _, p := range got {
		if filepath.Base(p) == "x.go" {
			t.Errorf("should skip worktrees dir, got %v", got)
		}
	}
}
