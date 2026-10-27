package approval

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingApprover struct {
	calls   int
	verdict Decision
	err     error
}

func (r *recordingApprover) Request(_ context.Context, _, _, _ string) (Decision, error) {
	r.calls++
	return r.verdict, r.err
}

func TestCache_Persistent_BypassesInner(t *testing.T) {
	rec := &recordingApprover{verdict: Deny}
	c := NewCache(rec, []string{"rm-recursive"}, nil)
	d, err := c.Request(context.Background(), "rm -rf x", "rm-recursive", "")
	if err != nil || d != AllowAlways {
		t.Fatalf("got d=%v err=%v, want AllowAlways", d, err)
	}
	if rec.calls != 0 {
		t.Fatalf("inner Approver called %d times; persistent should bypass", rec.calls)
	}
}

func TestCache_SessionCachesAfterFirst(t *testing.T) {
	rec := &recordingApprover{verdict: AllowSession}
	c := NewCache(rec, nil, nil)

	for i := range 3 {
		d, _ := c.Request(context.Background(), "rm -rf x", "rm-recursive", "")
		if d != AllowSession {
			t.Fatalf("iter %d: got %v", i, d)
		}
	}
	if rec.calls != 1 {
		t.Fatalf("inner called %d times; want 1 (rest from cache)", rec.calls)
	}
}

func TestCache_AllowOnceDoesNotCache(t *testing.T) {
	rec := &recordingApprover{verdict: AllowOnce}
	c := NewCache(rec, nil, nil)

	for range 3 {
		c.Request(context.Background(), "rm -rf x", "rm-recursive", "")
	}
	if rec.calls != 3 {
		t.Fatalf("inner called %d times; AllowOnce should not cache", rec.calls)
	}
}

func TestCache_AllowAlwaysCallsPersist(t *testing.T) {
	rec := &recordingApprover{verdict: AllowAlways}
	var mu sync.Mutex
	persisted := ""
	c := NewCache(rec, nil, func(k string) error {
		mu.Lock()
		defer mu.Unlock()
		persisted = k
		return nil
	})

	c.Request(context.Background(), "rm -rf x", "rm-recursive", "")
	// persist runs async; wait for it to flush before reading.
	c.Flush()
	mu.Lock()
	got := persisted
	mu.Unlock()
	if got != "rm-recursive" {
		t.Fatalf("persistFunc got %q; want rm-recursive", got)
	}
	// Second call should be served from persistent cache.
	c.Request(context.Background(), "rm -rf x", "rm-recursive", "")
	if rec.calls != 1 {
		t.Fatalf("inner called %d times; second hit should be from persistent cache", rec.calls)
	}
}

func TestCache_DenyPropagates(t *testing.T) {
	rec := &recordingApprover{verdict: Deny}
	c := NewCache(rec, nil, nil)
	d, _ := c.Request(context.Background(), "rm -rf /", "rm-recursive", "")
	if d != Deny {
		t.Fatalf("got %v, want Deny", d)
	}
	if len(c.persistent) != 0 {
		t.Fatal("deny should not seed allowlist")
	}
}

func TestCache_InnerError_DefaultsToDeny(t *testing.T) {
	rec := &recordingApprover{verdict: AllowAlways, err: errors.New("nope")}
	c := NewCache(rec, nil, nil)
	d, err := c.Request(context.Background(), "x", "k", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if d != Deny {
		t.Fatalf("got %v, want Deny on inner error", d)
	}
}

func TestStatic_AlwaysReturnsVerdict(t *testing.T) {
	s := Static{Verdict: AllowOnce}
	d, _ := s.Request(context.Background(), "x", "k", "d")
	if d != AllowOnce {
		t.Fatalf("got %v", d)
	}
}

func TestCache_PersistSerialized(t *testing.T) {
	rec := &recordingApprover{verdict: AllowAlways}
	var running, overlap atomic.Int32
	c := NewCache(rec, nil, func(string) error {
		if running.Add(1) > 1 {
			overlap.Add(1)
		}
		time.Sleep(5 * time.Millisecond)
		running.Add(-1)
		return nil
	})
	for _, k := range []string{"a", "b", "c", "d"} {
		c.Request(context.Background(), "x", k, "")
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("Flush = %v", err)
	}
	if overlap.Load() != 0 {
		t.Fatal("persist calls overlapped; read-modify-write would race")
	}
}

func TestCache_PersistErrorSurfaced(t *testing.T) {
	rec := &recordingApprover{verdict: AllowAlways}
	boom := errors.New("disk full")
	c := NewCache(rec, nil, func(string) error { return boom })
	var mu sync.Mutex
	var heard []string
	c.OnPersistError(func(key string, err error) {
		mu.Lock()
		defer mu.Unlock()
		heard = append(heard, key+":"+err.Error())
	})
	d, err := c.Request(context.Background(), "x", "rm-recursive", "")
	if d != AllowAlways || err != nil {
		t.Fatalf("grant must still hold, got %v, %v", d, err)
	}
	if ferr := c.Flush(); !errors.Is(ferr, boom) {
		t.Fatalf("Flush = %v; want wrapped %v", ferr, boom)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(heard) != 1 || !strings.HasPrefix(heard[0], "rm-recursive:") {
		t.Fatalf("handler heard %v", heard)
	}
}
