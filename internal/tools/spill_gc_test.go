package tools

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruneOlderThan(t *testing.T) {
	dir := t.TempDir()
	old, fresh := filepath.Join(dir, "old.txt"), filepath.Join(dir, "fresh.txt")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * spillMaxAge)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	pruneOlderThan(dir, spillMaxAge)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old spill file survived")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh spill file removed: %v", err)
	}
}
