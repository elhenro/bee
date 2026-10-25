package hive

import (
	"context"
	"sync"
	"testing"
)

// TestQueen_HooksFireInOrder verifies every hook fires for a full run and that
// the phase ordering holds: plan → workers → review/verify → synthesize.
func TestQueen_HooksFireInOrder(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{
		`["t1", "t2"]`, // decompose
		"final",        // synthesize
	}}
	worker := &scriptedRunner{outputs: []string{"w-a", "w-b"}}
	reviewer := &scriptedRunner{outputs: []string{"FINDING: x.go:1 boom", "NONE", "NONE"}}

	var mu sync.Mutex
	var phases []string
	var workerStarts, workerDones int

	q := NewQueen(planner, []Runner{worker})
	q.Reviewers = []Runner{reviewer} // round-robins across the 3 dims
	q.MaxParallel = 1                // deterministic ordering for the assertions below
	q.Hooks = Hooks{
		OnPlan: func(p []SubTask) {
			mu.Lock()
			phases = append(phases, "plan")
			mu.Unlock()
			if len(p) != 2 {
				t.Errorf("OnPlan got %d sub-tasks, want 2", len(p))
			}
		},
		OnWorkerStart: func(int, SubTask) { mu.Lock(); workerStarts++; mu.Unlock() },
		OnWorkerDone:  func(int, Result) { mu.Lock(); workerDones++; mu.Unlock() },
		OnReview:      func(dim string, _ []string) { mu.Lock(); phases = append(phases, "review:"+dim); mu.Unlock() },
		OnVerify:      func(Finding) { mu.Lock(); phases = append(phases, "verify"); mu.Unlock() },
		OnSynthesize:  func() { mu.Lock(); phases = append(phases, "synthesize"); mu.Unlock() },
	}

	if _, err := q.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := []string{"plan", "review:correctness", "verify", "review:persistence", "review:integration", "synthesize"}; !equalStrs(phases, got) {
		t.Errorf("phase order = %v, want %v", phases, got)
	}
	if workerStarts != 2 || workerDones != 2 {
		t.Errorf("worker start/done = %d/%d, want 2/2", workerStarts, workerDones)
	}
}

// TestQueen_NilHooksNoPanic guards the zero-value Hooks path used by existing
// callers (bee swarm) — every callback nil must run cleanly.
func TestQueen_NilHooksNoPanic(t *testing.T) {
	planner := &scriptedRunner{outputs: []string{`["t1"]`, "final"}}
	worker := &scriptedRunner{outputs: []string{"w"}}
	q := NewQueen(planner, []Runner{worker}) // Hooks left zero
	if _, err := q.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
