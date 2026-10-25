package flock

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLockExcludesSecondHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	unlock := Lock(path)

	acquired := make(chan struct{})
	go func() {
		defer Lock(path)()
		close(acquired)
	}()
	select {
	case <-acquired:
		t.Fatal("second Lock succeeded while the first was held")
	case <-time.After(100 * time.Millisecond):
	}

	unlock()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("second Lock never succeeded after unlock")
	}
}

func TestLockIsBestEffort(t *testing.T) {
	// lock file cannot be created: Lock must hand back a usable no-op
	Lock(filepath.Join(t.TempDir(), "missing", "f"))()
}
