package bench

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// AppendLedger must create the parent dir, append one line per call, and round
// trip the record fields.
func TestAppendLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "ledger.jsonl")
	if err := AppendLedger(path, LedgerRecord{Label: "a", Model: "m1", Aggregate: 82.6}); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := AppendLedger(path, LedgerRecord{Label: "b", Model: "m2", Aggregate: 84.7}); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var recs []LedgerRecord
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r LedgerRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		recs = append(recs, r)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d rows, want 2", len(recs))
	}
	if recs[0].Label != "a" || recs[1].Model != "m2" || recs[1].Aggregate != 84.7 {
		t.Errorf("round trip mismatch: %+v", recs)
	}
}

// empty path disables logging without error.
func TestAppendLedgerDisabled(t *testing.T) {
	if err := AppendLedger("", LedgerRecord{Label: "x"}); err != nil {
		t.Fatalf("empty path should be a no-op, got %v", err)
	}
}
