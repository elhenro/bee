package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// step builds a one-call step: the single-tool-call shape.
func step(name string, args map[string]any) []types.ToolUse {
	return []types.ToolUse{{ID: name, Name: name, Input: args}}
}

// A distinct [a,b,a,b] oscillation emits exactly one signal.
func TestABAB_EmitsOnceForDistinctPair(t *testing.T) {
	tr := newCycleTracker()
	fired := 0
	for _, k := range []string{"a", "b", "a", "b"} {
		if tr.observeKey(k) {
			fired++
		}
	}
	if fired != 1 {
		t.Fatalf("want exactly 1 signal for [a,b,a,b], got %d", fired)
	}
}

// A continuous episode [a,b,a,b,a,b] must not re-emit. The phase-shifted
// window [b,a,b,a] must map to the same episode as [a,b,a,b].
func TestABAB_ContinuousEpisodeDoesNotReEmit(t *testing.T) {
	tr := newCycleTracker()
	fired := 0
	for _, k := range []string{"a", "b", "a", "b", "a", "b"} {
		if tr.observeKey(k) {
			fired++
		}
	}
	if fired != 1 {
		t.Fatalf("want exactly 1 signal across one continuous episode, got %d", fired)
	}
}

// Non-oscillating windows emit nothing: adjacent repeats, all-distinct, and an
// ABAC window (the third element breaks the pair).
func TestABAB_NonOscillationNoSignal(t *testing.T) {
	cases := map[string][]string{
		"aabb": {"a", "a", "b", "b"},
		"abcd": {"a", "b", "c", "d"},
		"abac": {"a", "b", "a", "c"},
	}
	for name, keys := range cases {
		t.Run(name, func(t *testing.T) {
			tr := newCycleTracker()
			fired := 0
			for _, k := range keys {
				if tr.observeKey(k) {
					fired++
				}
			}
			if fired != 0 {
				t.Fatalf("window %v must emit nothing, got %d signals", keys, fired)
			}
		})
	}
}

// A real read→edit→read→edit oscillation is detected from step batches.
func TestABAB_DetectsReadEditOscillation(t *testing.T) {
	tr := newCycleTracker()
	read := step("read", map[string]any{"path": "main.go"})
	edit := step("edit", map[string]any{"path": "main.go", "old": "x", "new": "y"})
	steps := [][]types.ToolUse{read, edit, read, edit}
	for i, s := range steps {
		got := tr.ObserveStep(s)
		want := i == len(steps)-1 // only the closing step signals
		if got != want {
			t.Fatalf("step %d: signal=%v, want %v", i, got, want)
		}
	}
}

// A batch key is deterministic for both single- and multi-call steps, and is
// independent of the order calls appear in. Distinct calls key distinctly.
func TestCycle_BatchKeyDeterministic(t *testing.T) {
	single := step("read", map[string]any{"path": "a"})
	if got := batchKey(single); got != batchKey(single) {
		t.Fatalf("single-call key not deterministic: %q vs %q", got, batchKey(single))
	}

	one := types.ToolUse{ID: "1", Name: "read", Input: map[string]any{"path": "a"}}
	two := types.ToolUse{ID: "2", Name: "edit", Input: map[string]any{"path": "b", "new": "z"}}
	three := types.ToolUse{ID: "3", Name: "ls", Input: map[string]any{"path": "."}}
	fwd := batchKey([]types.ToolUse{one, two, three})
	rev := batchKey([]types.ToolUse{three, two, one})
	if fwd != rev {
		t.Fatalf("multi-call key order-dependent: %q vs %q", fwd, rev)
	}
	if fwd == batchKey(single) {
		t.Fatalf("distinct batches must key distinctly")
	}
	if batchKey(nil) != batchKey([]types.ToolUse{}) {
		t.Fatalf("nil and empty steps must share the empty sentinel")
	}
}

// The ring never retains more than cycleWindow keys; after many steps it holds
// exactly the most recent four, oldest→newest.
func TestCycle_BufferBounded(t *testing.T) {
	tr := newCycleTracker()
	keys := []string{"k0", "k1", "k2", "k3", "k4", "k5", "k6", "k7", "k8", "k9"}
	for _, k := range keys {
		tr.observeKey(k)
	}
	if tr.n != cycleWindow {
		t.Fatalf("write counter must saturate at %d, got %d", cycleWindow, tr.n)
	}
	want := [cycleWindow]string{"k6", "k7", "k8", "k9"}
	if got := tr.window(); got != want {
		t.Fatalf("ring must hold the last %d keys %v, got %v", cycleWindow, want, got)
	}
}