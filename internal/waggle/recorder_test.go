package waggle

import (
	"sync"
	"testing"
)

func TestRecorder_RecordAndSnapshot(t *testing.T) {
	r := NewRecorder(10)
	r.Record(Call{Tool: "grep"})
	r.Record(Call{Tool: "read"})
	got := r.Calls()
	if len(got) != 2 || got[0].Tool != "grep" || got[1].Tool != "read" {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
}

func TestRecorder_RingDropsOldest(t *testing.T) {
	r := NewRecorder(3)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		r.Record(Call{Tool: name})
	}
	got := r.Calls()
	if len(got) != 3 {
		t.Fatalf("expected cap 3, got %d", len(got))
	}
	if got[0].Tool != "c" || got[2].Tool != "e" {
		t.Errorf("oldest not dropped: %+v", got)
	}
}

func TestRecorder_SnapshotIsCopy(t *testing.T) {
	r := NewRecorder(5)
	r.Record(Call{Tool: "x"})
	snap := r.Calls()
	snap[0].Tool = "mutated"
	if r.Calls()[0].Tool != "x" {
		t.Error("snapshot mutation leaked into recorder")
	}
}

func TestRecorder_ConcurrentRecord(t *testing.T) {
	r := NewRecorder(1000)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Record(Call{Tool: "t"})
		}()
	}
	wg.Wait()
	if len(r.Calls()) != 50 {
		t.Errorf("expected 50 records, got %d", len(r.Calls()))
	}
}
