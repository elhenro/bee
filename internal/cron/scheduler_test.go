package cron

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// errStub is a sentinel failure returned by the error-case runner.
var errStub = errors.New("stub failure")

// fakeClock is an injectable Clock so scheduler tests need no real time.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// AC: a job outside active hours is deferred and runs when the window opens.
func TestSchedulerDefersOutsideActiveHours(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Add(JobSpec{Name: "nightly", Schedule: "* * * * *", ActiveHours: "09:00-17:00"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ran := make(chan struct{}, 1)
	run := RunnerFunc(func(context.Context, *Job) error { ran <- struct{}{}; return nil })
	clk := &fakeClock{now: time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)}
	sc := New(clk, run, reg.Jobs())
	defer sc.Shutdown()

	if n := sc.Tick(clk.Now()); n != 0 {
		t.Fatalf("outside window launched %d runs, want 0", n)
	}
	select {
	case <-ran:
		t.Fatal("job ran outside active hours")
	default:
	}

	clk.Set(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC))
	if n := sc.Tick(clk.Now()); n != 1 {
		t.Fatalf("window open launched %d runs, want 1", n)
	}
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred job did not run when the window opened")
	}
}

// AC: two jobs sharing a resource key never run concurrently.
func TestSchedulerSerializesSharedResource(t *testing.T) {
	reg := NewRegistry()
	for _, name := range []string{"a", "b"} {
		if _, err := reg.Add(JobSpec{Name: name, Schedule: "* * * * *", ResourceKey: "git"}); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	var active, maxActive int32
	done := make(chan struct{}, 2)
	run := RunnerFunc(func(context.Context, *Job) error {
		cur := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&maxActive)
			if cur <= old || atomic.CompareAndSwapInt32(&maxActive, old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		done <- struct{}{}
		return nil
	})
	clk := &fakeClock{now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
	sc := New(clk, run, reg.Jobs())
	defer sc.Shutdown()

	if n := sc.Tick(clk.Now()); n != 2 {
		t.Fatalf("launched %d runs, want 2", n)
	}
	for range 2 {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("shared-resource runs did not finish")
		}
	}
	if got := atomic.LoadInt32(&maxActive); got != 1 {
		t.Fatalf("max concurrent runs for shared key = %d, want 1", got)
	}
}

// AC: a crashed job does not wedge the queue — the slot is released on
// completion, failure, and panic alike.
func TestSchedulerReleasesQueueAfterCrash(t *testing.T) {
	cases := []struct {
		name string
		run  RunnerFunc
	}{
		{"panic", func(context.Context, *Job) error { panic("kaboom") }},
		{"error", func(context.Context, *Job) error { return errStub }},
		{"ok", func(context.Context, *Job) error { return nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry()
			if _, err := reg.Add(JobSpec{Name: "job", Schedule: "* * * * *", ResourceKey: "git"}); err != nil {
				t.Fatalf("Add: %v", err)
			}
			finished := make(chan struct{})
			runner := RunnerFunc(func(ctx context.Context, j *Job) error {
				defer close(finished)
				return tc.run(ctx, j)
			})
			clk := &fakeClock{now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)}
			sc := New(clk, runner, reg.Jobs())
			defer sc.Shutdown()

			sc.Tick(clk.Now())
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				t.Fatal("job did not finish")
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			release, err := sc.queue.Acquire(ctx, "git")
			if err != nil {
				t.Fatalf("queue wedged after %s: %v", tc.name, err)
			}
			release()
		})
	}
}

// AC: the scheduler is driven by the injected clock, not the wall clock.
func TestSchedulerUsesInjectedClock(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Add(JobSpec{Name: "now", Schedule: "* * * * *"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	future := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: future}
	ran := make(chan struct{}, 1)
	sc := New(clk, RunnerFunc(func(context.Context, *Job) error { ran <- struct{}{}; return nil }), reg.Jobs())
	sc.interval = time.Hour
	sc.Start(context.Background())
	defer sc.Shutdown()

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("job did not fire from the injected clock")
	}
}

// AC: empty config leaves the scheduler idle with no goroutine leak.
func TestSchedulerEmptyConfigIdle(t *testing.T) {
	before := runtime.NumGoroutine()
	sc := New(&fakeClock{now: time.Now()}, nil, nil)
	sc.Start(context.Background())
	sc.Shutdown()
	if n := sc.Tick(time.Now()); n != 0 {
		t.Fatalf("empty scheduler launched %d runs", n)
	}
	if got := settleGoroutines(before); got > before {
		t.Fatalf("goroutines grew from %d to %d on empty config", before, got)
	}
}

// AC: a started scheduler shuts down cleanly with no goroutine leak.
func TestSchedulerShutdownNoLeak(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Add(JobSpec{Name: "later", Schedule: "0 0 1 1 *"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	before := runtime.NumGoroutine()
	sc := New(realClock{}, nil, reg.Jobs())
	sc.interval = time.Hour
	sc.Start(context.Background())
	sc.Shutdown()
	if got := settleGoroutines(before); got > before {
		t.Fatalf("goroutines grew from %d to %d after shutdown", before, got)
	}
}

// settleGoroutines polls briefly for the goroutine count to fall back to at
// most want, then returns the observed count.
func settleGoroutines(want int) int {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= want {
			return runtime.NumGoroutine()
		}
		time.Sleep(5 * time.Millisecond)
	}
	return runtime.NumGoroutine()
}

// first run follows the schedule: a job whose expression does not match the
// current minute must not fire on the first tick after start.
func TestSchedulerFirstRunHonorsSchedule(t *testing.T) {
	reg := NewRegistry()
	if _, err := reg.Add(JobSpec{Name: "yearly", Schedule: "0 0 1 1 *"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	clk := &fakeClock{now: time.Date(2026, 3, 1, 12, 30, 0, 0, time.UTC)}
	sc := New(clk, nil, reg.Jobs())
	defer sc.Shutdown()
	if n := sc.Tick(clk.Now()); n != 0 {
		t.Fatalf("yearly job fired %d runs on first tick, want 0", n)
	}
	clk.Set(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if n := sc.Tick(clk.Now()); n != 1 {
		t.Fatalf("yearly job launched %d runs at its minute, want 1", n)
	}
}
