package queen

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
)

func TestParseWinner(t *testing.T) {
	cases := []struct {
		raw      string
		n        int
		wantSlot int
		wantOK   bool
	}{
		{"thinking...\nWINNER: 2 — cleaner and complete", 3, 1, true},
		{"WINNER: 1", 2, 0, true},
		{"winner: 3 - best tests", 3, 2, true},
		{"no verdict here", 3, 0, false},
		{"WINNER: 9 — out of range", 3, 0, false},
		{"WINNER: x — not a number", 3, 0, false},
	}
	for _, c := range cases {
		slot, _, ok := parseWinner(c.raw, c.n)
		if ok != c.wantOK || (ok && slot != c.wantSlot) {
			t.Errorf("parseWinner(%q,%d) = (%d,%v), want (%d,%v)", c.raw, c.n, slot, ok, c.wantSlot, c.wantOK)
		}
	}
}

func TestParseWinnerTakesLastMarker(t *testing.T) {
	// format mentioned in reasoning, real answer at the end
	raw := "I should answer WINNER: <n>. After review, WINNER: 2 — focused diff"
	slot, reason, ok := parseWinner(raw, 3)
	if !ok || slot != 1 {
		t.Fatalf("want slot 1, got %d ok=%v", slot, ok)
	}
	if !strings.Contains(reason, "focused") {
		t.Fatalf("reason not captured: %q", reason)
	}
}

func TestJudgeShortCircuits(t *testing.T) {
	v, _ := Judge(context.Background(), nil, "m", "obj", nil)
	if v.WinnerIdx != -1 {
		t.Fatalf("no candidates should yield -1, got %d", v.WinnerIdx)
	}
	v, _ = Judge(context.Background(), nil, "m", "obj", []Candidate{{Idx: 5, Diff: "x"}})
	if v.WinnerIdx != 5 {
		t.Fatalf("single candidate should win by idx, got %d", v.WinnerIdx)
	}
}

// fakeProvider returns a canned completion for the judge.
type fakeProvider struct{ reply string }

func (f fakeProvider) Name() string { return "fake" }

func (f fakeProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: f.reply}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

func TestJudgePicksFromProvider(t *testing.T) {
	p := fakeProvider{reply: "WINNER: 2 — better"}
	cands := []Candidate{{Idx: 10, Diff: "a"}, {Idx: 20, Diff: "b"}}
	v, err := Judge(context.Background(), p, "m", "obj", cands)
	if err != nil {
		t.Fatal(err)
	}
	if v.WinnerIdx != 20 {
		t.Fatalf("want winner idx 20 (candidate 2), got %d", v.WinnerIdx)
	}
}

func TestJudgeFallsBackOnUnparseable(t *testing.T) {
	p := fakeProvider{reply: "I cannot decide"}
	cands := []Candidate{{Idx: 10, Diff: "a"}, {Idx: 20, Diff: "b"}}
	v, _ := Judge(context.Background(), p, "m", "obj", cands)
	if v.WinnerIdx != 10 {
		t.Fatalf("unparseable should default to first candidate (10), got %d", v.WinnerIdx)
	}
}
