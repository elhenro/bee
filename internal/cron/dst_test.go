package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseStepFromSingleValue(t *testing.T) {
	s, err := ParseSchedule("5/15 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	for min, want := range map[int]bool{0: false, 5: true, 20: true, 35: true, 50: true, 55: false} {
		if got := s.Match(base.Add(time.Duration(min) * time.Minute)); got != want {
			t.Errorf("minute %d match=%v want %v", min, got, want)
		}
	}
	// ranges and wildcards keep their meaning
	r, _ := ParseSchedule("10-20/5 * * * *")
	if r.Match(base.Add(30*time.Minute)) || !r.Match(base.Add(15*time.Minute)) {
		t.Error("range step regressed")
	}
}

func TestPinnedHourJobFiresOnceAcrossFallBack(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	reg := NewRegistry()
	if _, err := reg.Add(JobSpec{Name: "nightly", Schedule: "30 2 * * *"}); err != nil {
		t.Fatal(err)
	}
	var runs atomic.Int32
	run := RunnerFunc(func(context.Context, *Job) error { runs.Add(1); return nil })
	// 2026-10-25: 03:00 CEST falls back to 02:00 CET, so 02:30 happens twice
	first := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC) // 02:30 CEST
	second := first.Add(time.Hour)                          // 02:30 CET
	clk := &fakeClock{now: first.Add(-time.Minute).In(loc)}
	sc := New(clk, run, reg.Jobs())
	defer sc.Shutdown()

	sc.Tick(first.In(loc))
	waitRuns(t, &runs, 1)
	sc.Tick(second.In(loc))
	time.Sleep(50 * time.Millisecond)
	if n := runs.Load(); n != 1 {
		t.Fatalf("job ran %d times across the repeated hour, want 1", n)
	}
}

func waitRuns(t *testing.T, runs *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for runs.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("runs=%d want %d", runs.Load(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
