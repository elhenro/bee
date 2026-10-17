package sysload

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func fake(t *testing.T, vals ...float64) *int {
	t.Helper()
	calls := 0
	old, oldPoll := read, pollEvery
	t.Cleanup(func() { read, pollEvery = old, oldPoll })
	pollEvery = time.Millisecond
	read = func() (float64, bool) {
		v := vals[min(calls, len(vals)-1)]
		calls++
		return v * float64(runtime.NumCPU()), true
	}
	return &calls
}

func TestWaitHoldsUntilLoadDrops(t *testing.T) {
	calls := fake(t, 3, 3, 0.5)
	holds := 0
	if err := Wait(context.Background(), 2, func(float64) { holds++ }); err != nil {
		t.Fatal(err)
	}
	if *calls != 3 || holds != 1 {
		t.Fatalf("calls=%d holds=%d, want 3 reads and one hold notice", *calls, holds)
	}
}

func TestWaitDisabledAndCancel(t *testing.T) {
	calls := fake(t, 9)
	if err := Wait(context.Background(), 0, nil); err != nil || *calls != 0 {
		t.Fatalf("ratio 0 must not read load: err=%v calls=%d", err, *calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Wait(ctx, 1, nil); err == nil {
		t.Fatal("canceled ctx under high load must return an error")
	}
}

func TestPerCoreReal(t *testing.T) {
	if l, ok := PerCore(); ok && l < 0 {
		t.Fatalf("negative load %v", l)
	}
}
