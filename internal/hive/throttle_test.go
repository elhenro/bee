package hive

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// gateWait installs a loadWait that holds until release is closed or the
// caller's own release predicate fires, and counts calls, so tests never
// depend on real machine load.
func gateWait(t *testing.T) (release chan struct{}, calls *int32) {
	t.Helper()
	release = make(chan struct{})
	calls = new(int32)
	prev := loadWait
	loadWait = func(ctx context.Context, _ float64, onHold func(float64), free func() bool) error {
		atomic.AddInt32(calls, 1)
		if onHold != nil {
			onHold(9)
		}
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			case <-tick.C:
				if free != nil && free() {
					return nil
				}
			}
		}
	}
	t.Cleanup(func() { loadWait = prev })
	return release, calls
}

// a held machine still runs one worker: holding the only one would stall.
func TestPool_LoneWorkerNeverWaits(t *testing.T) {
	_, calls := gateWait(t)
	withRunnerHook(t, func(*Worker) Runner { return &scriptedRunner{outputs: []string{"x"}} })
	p := NewPool(1)
	p.Throttle = Throttle{MaxLoadRatio: 1}
	p.Submit(&Worker{ID: "w0", Name: "w0", Task: "t"})
	got := Collect(p.Run(context.Background()))
	if len(got) != 1 || got[0].Err != nil || atomic.LoadInt32(calls) != 0 {
		t.Fatalf("lone worker must start at once: %+v waits=%d", got, *calls)
	}
}

func TestPool_ThrottleHoldsExtraWorkerUntilReleased(t *testing.T) {
	release, calls := gateWait(t)
	var inflight, maxSeen int32
	withRunnerHook(t, func(*Worker) Runner {
		return &stubRunner{final: "ok", delay: time.Second, inflight: &inflight, maxSeen: &maxSeen}
	})
	p := NewPool(2)
	var holds int32
	p.Throttle = Throttle{MaxLoadRatio: 1, OnHold: func(float64) { atomic.AddInt32(&holds, 1) }}
	p.Submit(&Worker{ID: "w0", Name: "w0", Task: "t"})
	p.Submit(&Worker{ID: "w1", Name: "w1", Task: "t"})
	out := p.Run(context.Background())

	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&inflight) != 1 {
		t.Fatalf("inflight=%d, want one worker running and one held", inflight)
	}
	close(release)
	got := Collect(out)
	if len(got) != 2 || got[0].Err != nil || got[1].Err != nil {
		t.Fatalf("both workers must finish after release: %+v", got)
	}
	if atomic.LoadInt32(calls) != 1 || atomic.LoadInt32(&holds) != 1 {
		t.Fatalf("waits=%d holds=%d, want 1 each", *calls, holds)
	}
}

func TestPool_ThrottleCancelIsCanceledResult(t *testing.T) {
	gateWait(t)
	withRunnerHook(t, func(*Worker) Runner {
		var a, b int32
		return &stubRunner{final: "ok", delay: time.Second, inflight: &a, maxSeen: &b}
	})
	p := NewPool(2)
	p.Throttle = Throttle{MaxLoadRatio: 1}
	p.Submit(&Worker{ID: "w0", Name: "w0", Task: "t"})
	p.Submit(&Worker{ID: "w1", Name: "w1", Task: "t"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	for _, r := range Collect(p.Run(ctx)) {
		if r.Err == nil {
			t.Fatalf("canceled run must report ctx errors: %+v", r)
		}
	}
}

func TestThrottle_ZeroRatioNeverWaits(t *testing.T) {
	_, calls := gateWait(t)
	var running atomic.Int32
	running.Store(3)
	end, err := (Throttle{}).start(context.Background(), &running)
	if err != nil {
		t.Fatal(err)
	}
	end()
	if atomic.LoadInt32(calls) != 0 || running.Load() != 3 {
		t.Fatalf("ratio 0 must not consult the load (waits=%d running=%d)", *calls, running.Load())
	}
}

// a held worker goes as soon as its siblings finish, without the load ever
// dropping: the hive slows down under load but never stalls.
func TestQueen_FactoryHeldWorkerStartsWhenSiblingsFinish(t *testing.T) {
	_, calls := gateWait(t)
	planner := &scriptedRunner{outputs: []string{`["a","b"]`, "summary"}}
	var spawned int32
	q := NewQueen(planner, nil)
	q.MaxParallel = 2
	q.Throttle = Throttle{MaxLoadRatio: 1}
	q.WorkerFor = func(int, SubTask) (Runner, func(error), error) {
		atomic.AddInt32(&spawned, 1)
		var a, b int32
		return &stubRunner{final: "out", delay: 20 * time.Millisecond, inflight: &a, maxSeen: &b}, nil, nil
	}
	done := make(chan error, 1)
	go func() { _, err := q.Run(context.Background(), "task"); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("held worker never started after its sibling finished")
	}
	if spawned != 2 || atomic.LoadInt32(calls) != 1 {
		t.Fatalf("spawned=%d waits=%d, want 2 spawned and the second held once", spawned, *calls)
	}
}
