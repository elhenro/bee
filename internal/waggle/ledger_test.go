package waggle

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLedger_AppendAndAggregate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := NewLedger(path)
	if err := l.Append(LedgerEntry{Name: "wag_a", Steps: 2, Yield: 100}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(LedgerEntry{Name: "wag_a", Steps: 2, Yield: 50}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(LedgerEntry{Name: "wag_b", Steps: 1, Yield: 30}); err != nil {
		t.Fatal(err)
	}
	stats, err := ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats["wag_a"].Uses != 2 || stats["wag_a"].Yield != 150 {
		t.Errorf("wag_a aggregate wrong: %+v", stats["wag_a"])
	}
	if stats["wag_b"].Uses != 1 || stats["wag_b"].Yield != 30 {
		t.Errorf("wag_b aggregate wrong: %+v", stats["wag_b"])
	}
}

func TestReadLedger_AggregatesFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := NewLedger(path)
	_ = l.Append(LedgerEntry{Name: "wag_d", Steps: 2, Yield: 80})  // a success
	_ = l.Append(LedgerEntry{Name: "wag_d", Steps: 2, Fail: true}) // a divergence
	_ = l.Append(LedgerEntry{Name: "wag_d", Steps: 2, Fail: true}) // another divergence
	stats, err := ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	got := stats["wag_d"]
	if got.Uses != 1 || got.Yield != 80 || got.Fails != 2 {
		t.Errorf("fail aggregation wrong: %+v", got)
	}
}

func TestReadLedger_MissingIsEmpty(t *testing.T) {
	stats, err := ReadLedger(filepath.Join(t.TempDir(), "none.jsonl"))
	if err != nil || len(stats) != 0 {
		t.Fatalf("missing ledger should be empty: %v %v", stats, err)
	}
}

func TestCompactLedger_DropsOrphanEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	l := NewLedger(path)
	_ = l.Append(LedgerEntry{Name: "gone", Fail: true})
	_ = l.Append(LedgerEntry{Name: "gone", Fail: true})
	_ = l.Append(LedgerEntry{Name: "kept", Yield: 50})
	if err := CompactLedger(path, map[string]bool{"kept": true}); err != nil {
		t.Fatal(err)
	}
	stats, _ := ReadLedger(path)
	if _, ok := stats["gone"]; ok {
		t.Error("orphan entries should be compacted away")
	}
	if stats["kept"].Uses != 1 || stats["kept"].Yield != 50 {
		t.Errorf("surviving entries must be preserved: %+v", stats["kept"])
	}
}

func TestCompactLedger_MissingIsNoop(t *testing.T) {
	if err := CompactLedger(filepath.Join(t.TempDir(), "none.jsonl"), nil); err != nil {
		t.Fatalf("missing ledger compact should be no-op: %v", err)
	}
}

func TestStore_LedgerPathSiblingOfSkills(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	s, _ := ProjectStore("/p")
	want := filepath.Join(filepath.Dir(s.Dir()), "ledger.jsonl")
	if s.LedgerPath() != want {
		t.Errorf("ledger path = %q, want %q", s.LedgerPath(), want)
	}
}

func TestReplay_WritesLedgerOnFire(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	rt := litRoute()
	rt.Scope = ScopeProject
	r := NewReplayer([]Route{rt}, 2)
	r.SetLedger(ScopeProject, NewLedger(path))
	r.Observe(Call{Tool: "ls", Args: map[string]string{"path": "internal"}})
	r.Observe(rd("a.go"))
	if _, ok := r.Follow(context.Background(), okExec("PREFETCH")); !ok {
		t.Fatal("expected replay to fire")
	}
	stats, err := ReadLedger(path)
	if err != nil {
		t.Fatal(err)
	}
	if stats["wag_x"].Uses != 1 || stats["wag_x"].Yield <= 0 {
		t.Errorf("ledger not written on fire: %+v", stats["wag_x"])
	}
}
