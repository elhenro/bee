package cron

import (
	"context"
	"testing"
	"time"
)

// busy reports whether key has a holder: the slot token is out.
func busy(q *Queue, key string) bool { return len(q.locks[key]) == 0 }

func TestQueueSerializesPerKey(t *testing.T) {
	q := NewQueue()
	release, err := q.Acquire(context.Background(), "git")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !busy(q, "git") {
		t.Fatal("expected key to be busy while held")
	}

	acquired := make(chan func(), 1)
	go func() {
		rel, err := q.Acquire(context.Background(), "git")
		if err != nil {
			acquired <- nil
			return
		}
		acquired <- rel
	}()

	select {
	case <-acquired:
		t.Fatal("second Acquire returned while key was held")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case rel := <-acquired:
		if rel == nil {
			t.Fatal("second Acquire failed after release")
		}
		rel()
	case <-time.After(2 * time.Second):
		t.Fatal("second Acquire did not proceed after release")
	}
}

func TestQueueIndependentKeysRunConcurrently(t *testing.T) {
	q := NewQueue()
	relA, err := q.Acquire(context.Background(), "a")
	if err != nil {
		t.Fatalf("Acquire a: %v", err)
	}
	defer relA()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	relB, err := q.Acquire(ctx, "b")
	if err != nil {
		t.Fatalf("Acquire b blocked on an unrelated key: %v", err)
	}
	relB()
}

func TestQueueReleaseIsIdempotent(t *testing.T) {
	q := NewQueue()
	release, err := q.Acquire(context.Background(), "k")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()
	release() // must not double-fill the slot
	if busy(q, "k") {
		t.Fatal("key should be free after release")
	}
}

func TestQueueAcquireHonorsContext(t *testing.T) {
	q := NewQueue()
	release, err := q.Acquire(context.Background(), "k")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := q.Acquire(ctx, "k"); err == nil {
		t.Fatal("expected context cancellation error")
	}
}
