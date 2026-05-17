package hive

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/loop"
)

// stubRunner is a Runner that returns canned output after a configurable
// delay. Tracks how many concurrent calls are in flight so tests can assert
// MaxConcurrency is honored.
type stubRunner struct {
	final     string
	err       error
	delay     time.Duration
	inflight  *int32
	maxSeen   *int32
	startedAt *int64 // unix nano
}

func (s *stubRunner) Run(ctx context.Context, _ string) (loop.RunResult, error) {
	n := atomic.AddInt32(s.inflight, 1)
	defer atomic.AddInt32(s.inflight, -1)
	for {
		old := atomic.LoadInt32(s.maxSeen)
		if n <= old || atomic.CompareAndSwapInt32(s.maxSeen, old, n) {
			break
		}
	}
	if s.startedAt != nil {
		atomic.CompareAndSwapInt64(s.startedAt, 0, time.Now().UnixNano())
	}
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return loop.RunResult{}, ctx.Err()
		}
	}
	if s.err != nil {
		return loop.RunResult{FinalText: s.final}, s.err
	}
	return loop.RunResult{FinalText: s.final}, nil
}

// withRunnerHook installs a runnerHook for the duration of the test and
// restores the previous value on cleanup. Avoids leaking state between tests.
func withRunnerHook(t *testing.T, hook func(*Worker) Runner) {
	t.Helper()
	prev := runnerHook
	runnerHook = hook
	t.Cleanup(func() { runnerHook = prev })
}

func TestPool_RunAllResults(t *testing.T) {
	var inflight, maxSeen int32
	runners := map[string]*stubRunner{}
	withRunnerHook(t, func(w *Worker) Runner { return runners[w.ID] })

	p := NewPool(3)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("w%d", i)
		runners[id] = &stubRunner{
			final:    fmt.Sprintf("final-%d", i),
			delay:    5 * time.Millisecond,
			inflight: &inflight,
			maxSeen:  &maxSeen,
		}
		p.Submit(&Worker{ID: id, Name: id, Task: "t"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got := Collect(p.Run(ctx))
	if len(got) != 5 {
		t.Fatalf("want 5 results, got %d", len(got))
	}
	seen := map[string]bool{}
	for _, r := range got {
		if r.Err != nil {
			t.Errorf("worker %s err: %v", r.Name, r.Err)
		}
		seen[r.WorkerID] = true
	}
	if len(seen) != 5 {
		t.Errorf("expected 5 distinct workers, got %d", len(seen))
	}
}

func TestPool_RespectsMaxConcurrency(t *testing.T) {
	var inflight, maxSeen int32
	runners := map[string]*stubRunner{}
	withRunnerHook(t, func(w *Worker) Runner { return runners[w.ID] })

	const cap = 2
	p := NewPool(cap)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("w%d", i)
		runners[id] = &stubRunner{
			final:    "x",
			delay:    20 * time.Millisecond,
			inflight: &inflight,
			maxSeen:  &maxSeen,
		}
		p.Submit(&Worker{ID: id, Name: id, Task: "t"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got := Collect(p.Run(ctx))
	if len(got) != 8 {
		t.Fatalf("want 8 results, got %d", len(got))
	}
	if seen := atomic.LoadInt32(&maxSeen); seen > cap {
		t.Errorf("MaxConcurrency=%d exceeded: saw %d in flight", cap, seen)
	}
}

func TestPool_ContextCancellation(t *testing.T) {
	var inflight, maxSeen int32
	runners := map[string]*stubRunner{}
	withRunnerHook(t, func(w *Worker) Runner { return runners[w.ID] })

	p := NewPool(2)
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("w%d", i)
		runners[id] = &stubRunner{
			final:    "x",
			delay:    500 * time.Millisecond,
			inflight: &inflight,
			maxSeen:  &maxSeen,
		}
		p.Submit(&Worker{ID: id, Name: id, Task: "t"})
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch := p.Run(ctx)

	// cancel almost immediately to interrupt in-flight + drop queued.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	got := Collect(ch)
	if len(got) != 4 {
		t.Fatalf("want 4 results (including canceled), got %d", len(got))
	}
	var errs int
	for _, r := range got {
		if r.Err != nil {
			errs++
		}
	}
	if errs == 0 {
		t.Errorf("expected at least one canceled/error result, got 0")
	}
	// final pool state: no worker stays in running/pending after Run returns
	for _, w := range p.WorkersSnapshot() {
		if w.State == StateRunning || w.State == StatePending {
			t.Errorf("worker %s stuck in state %s after Run", w.ID, w.State)
		}
	}
}

func TestPool_ChannelCloses(t *testing.T) {
	withRunnerHook(t, func(w *Worker) Runner {
		return &stubRunner{final: "ok", inflight: new(int32), maxSeen: new(int32)}
	})

	p := NewPool(2)
	p.Submit(&Worker{ID: "a", Name: "a", Task: "t"})
	p.Submit(&Worker{ID: "b", Name: "b", Task: "t"})

	ch := p.Run(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range ch {
		}
	}()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("results channel did not close")
	}
}

func TestPool_NilEngineProducesError(t *testing.T) {
	// no hook installed: w.Engine is nil → runOne returns nilEngineErr.
	p := NewPool(1)
	p.Submit(&Worker{ID: "x", Name: "x", Task: "t"})
	got := Collect(p.Run(context.Background()))
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Err == nil {
		t.Fatalf("expected error for nil engine, got nil")
	}
	if !errors.Is(got[0].Err, errNilEngine) && got[0].Err.Error() != "hive: worker has nil engine" {
		t.Errorf("unexpected err: %v", got[0].Err)
	}
}

func TestNewPool_MinClamps(t *testing.T) {
	if got := NewPool(0).MaxConcurrency; got != 1 {
		t.Errorf("NewPool(0) MaxConcurrency = %d, want 1", got)
	}
	if got := NewPool(-3).MaxConcurrency; got != 1 {
		t.Errorf("NewPool(-3) MaxConcurrency = %d, want 1", got)
	}
}
