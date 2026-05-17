package bgreg

import (
	"testing"
	"time"
)

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
