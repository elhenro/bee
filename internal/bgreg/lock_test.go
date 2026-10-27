package bgreg

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSessionLock_MutualExclusion verifies that two concurrent acquireSessionLock
// callers don't both hold the lock at the same time. One waits while the
// other holds; release lets the second proceed.
func TestSessionLock_MutualExclusion(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	const id = "lock-mutex"

	var holders atomic.Int32
	var maxHolders int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			l, err := acquireSessionLock(id)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			defer l.release()
			cur := holders.Add(1)
			for {
				m := atomic.LoadInt32(&maxHolders)
				if cur <= m || atomic.CompareAndSwapInt32(&maxHolders, m, cur) {
					break
				}
			}
			// hold briefly so the contender has to wait
			time.Sleep(50 * time.Millisecond)
			holders.Add(-1)
		})
	}
	wg.Wait()
	if maxHolders != 1 {
		t.Fatalf("max concurrent holders=%d; want 1", maxHolders)
	}
}

// release must keep the lock file: unlinking it lets a waiter that already
// opened the old inode and a newcomer that creates a new one both hold it.
func TestSessionLock_ReleaseKeepsFileRemoveDropsIt(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	const id = "lock-keep"
	l, err := acquireSessionLock(id)
	if err != nil {
		t.Fatal(err)
	}
	l.release()
	if _, err := os.Stat(l.path); err != nil {
		t.Fatalf("lock file unlinked on release: %v", err)
	}
	if err := Remove(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(l.path); !os.IsNotExist(err) {
		t.Fatalf("Remove left the lock file behind: %v", err)
	}
}

// TestSessionLock_FailsFastWhenHeldTooLong verifies that the 2s deadline
// fires when a holder doesn't release within the window. We hold one for
// longer than the deadline and expect the contender to error out.
func TestSessionLock_FailsFastWhenHeldTooLong(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	const id = "lock-deadline"

	held, err := acquireSessionLock(id)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer held.release()

	start := time.Now()
	_, err = acquireSessionLock(id)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("second acquire succeeded; want contention error")
	}
	if elapsed < 1500*time.Millisecond || elapsed > 4*time.Second {
		t.Fatalf("contention deadline timing off: %v", elapsed)
	}
}
