package zzz

import (
	"testing"
	"time"
)

func TestPrune_KeepNewest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	now := time.Now().UTC()
	for i, st := range []string{StatusCompleted, StatusCompleted, StatusCompleted, StatusFailed, StatusAborted} {
		r := &Run{
			ID:        NewID() + "-" + string(rune('a'+i)),
			Status:    st,
			StartedAt: now.Add(-time.Duration(i+1) * time.Hour),
			EndedAt:   now.Add(-time.Duration(i+1) * time.Hour),
		}
		if err := SaveMeta(r); err != nil {
			t.Fatalf("save: %v", err)
		}
		// avoid id collisions
		time.Sleep(2 * time.Millisecond)
	}

	res := Prune(PruneOpts{KeepNewest: 2, MaxAge: time.Nanosecond})
	if len(res.RemovedRunIDs) != 3 {
		t.Errorf("removed=%d want 3 (5 terminal - keep 2)", len(res.RemovedRunIDs))
	}
	remaining, _ := ListRuns()
	if len(remaining) != 2 {
		t.Errorf("remaining=%d want 2", len(remaining))
	}
}

func TestPrune_RetainsActive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	now := time.Now().UTC()
	active := &Run{
		ID:        "active-1",
		Status:    StatusRunning,
		StartedAt: now.Add(-30 * 24 * time.Hour),
		EndedAt:   time.Time{},
	}
	if err := SaveMeta(active); err != nil {
		t.Fatalf("save active: %v", err)
	}

	res := Prune(PruneOpts{MaxAge: time.Hour, KeepNewest: 1})
	if len(res.RemovedRunIDs) != 0 {
		t.Errorf("running run removed: %v", res.RemovedRunIDs)
	}
}
