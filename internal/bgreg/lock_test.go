package bgreg

import (
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

	var holders int32
	var maxHolders int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, err := acquireSessionLock(id)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			defer l.release()
			cur := atomic.AddInt32(&holders, 1)
			for {
				m := atomic.LoadInt32(&maxHolders)
				if cur <= m || atomic.CompareAndSwapInt32(&maxHolders, m, cur) {
					break
				}
			}
			// hold briefly so the contender has to wait
			time.Sleep(50 * time.Millisecond)
			atomic.AddInt32(&holders, -1)
		}()
	}
	wg.Wait()
	if maxHolders != 1 {
		t.Fatalf("max concurrent holders=%d; want 1", maxHolders)
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
