//go:build !windows

package pidlock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestExclusiveAndReleaseKeepsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	l, err := Acquire(p, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Acquire(p, "run-2")
	var held *HeldError
	if !errors.As(err, &held) || !strings.Contains(held.Info, "run-1") {
		t.Fatalf("want HeldError naming run-1, got %v", err)
	}
	if !Held(p) {
		t.Fatal("Held = false while locked")
	}
	l.Release()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("release must not unlink the lock file: %v", err)
	}
	if Held(p) {
		t.Fatal("Held = true after release")
	}
	l2, err := Acquire(p, "run-3")
	if err != nil {
		t.Fatal(err)
	}
	l2.Release()
}

func TestStaleRecordDoesNotBlock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	if err := os.WriteFile(p, []byte("999999\nold\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(p, "new")
	if err != nil {
		t.Fatalf("a crashed holder leaves no flock: %v", err)
	}
	l.Release()
}

func TestNeverTwoHolders(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.lock")
	var holders, maxHolders atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			l, err := Acquire(p, "r")
			if err != nil {
				return
			}
			n := holders.Add(1)
			for {
				m := maxHolders.Load()
				if n <= m || maxHolders.CompareAndSwap(m, n) {
					break
				}
			}
			holders.Add(-1)
			l.Release()
		})
	}
	wg.Wait()
	if maxHolders.Load() > 1 {
		t.Fatalf("%d concurrent holders", maxHolders.Load())
	}
}
