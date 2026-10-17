package hive

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// gateWait installs a loadWait that blocks until release is closed and counts
// calls, so tests never depend on real machine load.
func gateWait(t *testing.T) (release chan struct{}, calls *int32) {
	t.Helper()
	release = make(chan struct{})
	calls = new(int32)
	prev := loadWait
	loadWait = func(ctx context.Context, _ float64, onHold func(float64)) error {
		atomic.AddInt32(calls, 1)
		if onHold != nil {
			onHold(9)
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t.Cleanup(func() { loadWait = prev })
	return release, calls
}

func TestPool_ThrottleHoldsStartUntilReleased(t *testing.T) {
	release, _ := gateWait(t)
	var inflight, maxSeen int32
	var started int64
	withRunnerHook(t, func(*Worker) Runner {
		return &stubRunner{final: "ok", inflight: &inflight, maxSeen: &maxSeen, startedAt: &started}
	})
	p := NewPool(1)
	var holds int32
	p.Throttle = Throttle{MaxLoadRatio: 1, OnHold: func(float64) { atomic.AddInt32(&holds, 1) }}
	p.Submit(&Worker{ID: "w0", Name: "w0", Task: "t"})
	out := p.Run(context.Background())

	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt64(&started) != 0 {
		t.Fatal("worker started while the throttle held it")
	}
	close(release)
	got := Collect(out)
	if len(got) != 1 || got[0].Err != nil || atomic.LoadInt64(&started) == 0 {
		t.Fatalf("worker did not run after release: %+v", got)
	}
	if atomic.LoadInt32(&holds) != 1 {
		t.Fatalf("OnHold fired %d times, want 1", holds)
	}
}

func TestPool_ThrottleCancelIsCanceledResult(t *testing.T) {
	gateWait(t)
	withRunnerHook(t, func(*Worker) Runner { return &scriptedRunner{outputs: []string{"x"}} })
	p := NewPool(1)
	p.Throttle = Throttle{MaxLoadRatio: 1}
	p.Submit(&Worker{ID: "w0", Name: "w0", Task: "t"})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	got := Collect(p.Run(ctx))
	if len(got) != 1 || got[0].Err == nil {
		t.Fatalf("held worker canceled mid-wait must report ctx error: %+v", got)
	}
}

func TestThrottle_ZeroRatioNeverWaits(t *testing.T) {
	_, calls := gateWait(t)
	if err := (Throttle{}).wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Fatal("ratio 0 must not consult the load")
	}
}

func TestQueen_FactoryThrottleHoldsWorkers(t *testing.T) {
	release, calls := gateWait(t)
	planner := &scriptedRunner{outputs: []string{`["a","b"]`, "summary"}}
	var spawned int32
	q := NewQueen(planner, nil)
	q.Throttle = Throttle{MaxLoadRatio: 1}
	q.WorkerFor = func(int, SubTask) (Runner, func(error), error) {
		atomic.AddInt32(&spawned, 1)
		return &scriptedRunner{outputs: []string{"out"}}, nil, nil
	}
	done := make(chan error, 1)
	go func() { _, err := q.Run(context.Background(), "task"); done <- err }()

	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&spawned) != 0 {
		t.Fatal("factory spawned a worker while the throttle held it")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if spawned != 2 || atomic.LoadInt32(calls) != 2 {
		t.Fatalf("spawned=%d waits=%d, want 2 each", spawned, *calls)
	}
}
