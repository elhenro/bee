package hive

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/elhenro/bee/internal/loop"
)

// TestQueen_WorkerForRunsEverySubTask checks the WorkerFor path spawns one
// runner per sub-task, runs them all, and reports each done callback with the
// worker's error.
func TestQueen_WorkerForRunsEverySubTask(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`["a","b","c"]`,
		"summary",
	}}

	var spawned, doneCalls int32
	var doneErrs sync.Map // idx -> error

	q := NewQueen(planner, nil)
	q.MaxParallel = 2
	q.WorkerFor = func(idx int, _ SubTask) (Runner, func(error), error) {
		atomic.AddInt32(&spawned, 1)
		r := &scriptedRunner{outputs: []string{"out"}}
		if idx == 1 {
			r = &scriptedRunner{errAfter: 1}
		}
		done := func(err error) {
			atomic.AddInt32(&doneCalls, 1)
			doneErrs.Store(idx, err)
		}
		return r, done, nil
	}

	res, err := q.Run(context.Background(), "task")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if spawned != 3 {
		t.Fatalf("spawned = %d, want 3", spawned)
	}
	if doneCalls != 3 {
		t.Fatalf("doneCalls = %d, want 3", doneCalls)
	}
	if got, _ := doneErrs.Load(1); got == nil {
		t.Errorf("worker 1 done err = nil, want the scripted failure")
	}
	if res.WorkerResults[1].Err == nil {
		t.Errorf("WorkerResults[1].Err = nil, want failure recorded")
	}
	if res.WorkerResults[0].Final != "out" {
		t.Errorf("WorkerResults[0].Final = %q, want %q", res.WorkerResults[0].Final, "out")
	}
}

// TestQueen_WorkerForRespectsMaxParallel checks no more than MaxParallel
// workers run at once.
func TestQueen_WorkerForRespectsMaxParallel(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`["a","b","c","d","e"]`,
		"summary",
	}}

	var inFlight, peak int32
	gate := make(chan struct{})

	q := NewQueen(planner, nil)
	q.MaxParallel = 2
	q.WorkerFor = func(_ int, _ SubTask) (Runner, func(error), error) {
		return runnerFunc(func(context.Context, string) (loop.RunResult, error) {
			cur := atomic.AddInt32(&inFlight, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
					break
				}
			}
			<-gate
			atomic.AddInt32(&inFlight, -1)
			return loop.RunResult{FinalText: "ok"}, nil
		}), nil, nil
	}

	// release all workers shortly after they pile up against the cap.
	go func() {
		for atomic.LoadInt32(&peak) < 2 {
		}
		close(gate)
	}()

	if _, err := q.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if peak > 2 {
		t.Fatalf("peak concurrency = %d, want <= 2", peak)
	}
}

func TestRole_ReadOnly(t *testing.T) {
	readOnly := []Role{RoleSage, RoleArchivist, RoleForager, RoleDiviner, RoleCritic, RoleEye}
	for _, r := range readOnly {
		if !r.ReadOnly() {
			t.Errorf("%s.ReadOnly() = false, want true", r)
		}
	}
	mutating := []Role{RoleBuilder, RoleForeman, RoleScoutPlanner, RoleQueen, RoleSubqueen}
	for _, r := range mutating {
		if r.ReadOnly() {
			t.Errorf("%s.ReadOnly() = true, want false", r)
		}
	}
	if Role("nonsense").ReadOnly() {
		t.Errorf("unknown role ReadOnly() = true, want false (isolate by default)")
	}
}
