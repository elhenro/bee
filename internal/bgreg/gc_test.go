package bgreg

import (
	"testing"
	"time"
)

func TestPrune_KeepsUnmergedAgentWork(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	old := time.Now().UTC().Add(-48 * time.Hour)
	for _, s := range []Status{
		{SessionID: "stranded", State: StateDone, WorktreePath: "/w/1", MergeState: MergeStateUnmerged},
		{SessionID: "blocked", State: StateFailed, WorktreePath: "/w/2", MergeState: MergeStateBlocked},
		{SessionID: "merged", State: StateDone, WorktreePath: "/w/3", MergeState: MergeStateMerged},
		{SessionID: "legacy", State: StateDone},
	} {
		s.UpdatedAt, s.FinishedAt = old, old
		if err := Write(s); err != nil {
			t.Fatal(err)
		}
	}
	res := Prune(PruneOpts{MaxAge: time.Hour})
	if len(res.RemovedIDs) != 2 {
		t.Fatalf("removed %v, want only merged and legacy", res.RemovedIDs)
	}
	for _, id := range []string{"stranded", "blocked"} {
		if _, err := Read(id); err != nil {
			t.Errorf("%s pruned: %v", id, err)
		}
	}
}

func TestPrune_KeepNewestAndAge(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)

	now := time.Now().UTC()
	// seed 4 done + 1 active. KeepNewest=1, MaxAge=1ns → drop 3.
	seed := []struct {
		id    string
		state State
		t     time.Time
	}{
		{"d1", StateDone, now.Add(-4 * time.Hour)},
		{"d2", StateDone, now.Add(-3 * time.Hour)},
		{"d3", StateFailed, now.Add(-2 * time.Hour)},
		{"d4", StateDone, now.Add(-1 * time.Hour)},
		{"a1", StateActive, now}, // active — never pruned
	}
	for _, s := range seed {
		if err := Write(Status{SessionID: s.id, State: s.state, UpdatedAt: s.t, FinishedAt: s.t}); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}

	res := Prune(PruneOpts{KeepNewest: 1, MaxAge: time.Nanosecond})
	if len(res.RemovedIDs) != 3 {
		t.Errorf("removed=%d want 3 (4 terminal - 1 keep)", len(res.RemovedIDs))
	}
	// the active session must survive
	if _, err := Read("a1"); err != nil {
		t.Errorf("active session deleted: %v", err)
	}
}
