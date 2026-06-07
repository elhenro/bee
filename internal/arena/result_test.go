package arena

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendResultCreatesDirsAndAppendsLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "wars.jsonl") // nested dir must be created

	r1 := MatchResult{MatchID: "m1", Winner: "red", Reason: "exfiltration", Rounds: 3}
	r2 := MatchResult{MatchID: "m2", Winner: "blue", Reason: "self_bankrupt", Rounds: 7}
	if err := AppendResult(path, r1); err != nil {
		t.Fatalf("AppendResult r1: %v", err)
	}
	if err := AppendResult(path, r2); err != nil {
		t.Fatalf("AppendResult r2: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer f.Close()
	var got []MatchResult
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m MatchResult
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("bad ledger line %q: %v", sc.Text(), err)
		}
		got = append(got, m)
	}
	if len(got) != 2 {
		t.Fatalf("ledger has %d lines, want 2", len(got))
	}
	if got[0].MatchID != "m1" || got[1].MatchID != "m2" {
		t.Fatalf("lines out of order: %q then %q", got[0].MatchID, got[1].MatchID)
	}
}

func TestAppendResultEmptyPathNoop(t *testing.T) {
	if err := AppendResult("", MatchResult{}); err != nil {
		t.Fatalf("empty path should be a silent noop, got %v", err)
	}
}

func TestEloEqualRatingsWinnerGains(t *testing.T) {
	newA, newB := Elo(1500, 1500, 1.0, 24) // a beats equal b
	if newA != 1512 {
		t.Fatalf("newA = %v, want 1512", newA)
	}
	if newB != 1488 {
		t.Fatalf("newB = %v, want 1488", newB)
	}
}

func TestEloConservesTotalPoints(t *testing.T) {
	a, b := 1640.0, 1390.0
	newA, newB := Elo(a, b, 0.0, 32) // upset: lower-rated b wins
	if newB <= b {
		t.Fatalf("winner b should gain: %v -> %v", b, newB)
	}
	if newA >= a {
		t.Fatalf("loser a should drop: %v -> %v", a, newA)
	}
	if diff := (newA + newB) - (a + b); diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("elo not conserved: %v != %v (diff %v)", newA+newB, a+b, diff)
	}
}

func TestEloScoreMapping(t *testing.T) {
	if EloScore("red", "red") != 1.0 {
		t.Fatal("winner side should score 1.0")
	}
	if EloScore("red", "blue") != 0.0 {
		t.Fatal("losing side should score 0.0")
	}
	if EloScore("draw", "red") != 0.5 {
		t.Fatal("draw should score 0.5")
	}
}
