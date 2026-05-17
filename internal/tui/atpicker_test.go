package tui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtPicker_FilterMatches(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "internal", "tui"), 0o755)
	os.WriteFile(filepath.Join(dir, "internal", "tui", "app.go"), nil, 0o644)
	p := NewAtPicker(dir)
	if len(p.matches) == 0 {
		t.Fatal("want some initial matches")
	}
}
