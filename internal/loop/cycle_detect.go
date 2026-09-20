package loop

import "github.com/elhenro/bee/internal/types"

// ABAB-cycle (oscillation) detection. repeat_detect.go catches the same call
// twice in a row; it is blind to read→edit→read→edit alternation that makes no
// progress. This tracker watches the per-step action batch and flags a step
// whose batch closes an [a,b,a,b] oscillation of two distinct batch keys.
//
// A "step" is one iteration's batch of tool calls (the slice dispatchTools
// receives), so a single-tool step and a multi-tool step are both one key.
//
// The tracker is a pure signal source: it reports that an oscillation episode
// happened. It does not build reminder text or decide policy — that is the
// guard observer's job.

// cycleWindow is the ABAB lookback length: the last four step keys are
// [a,b,a,b].
const cycleWindow = 4

// cycleTracker keeps a bounded ring of the last cycleWindow step keys and
// emits at most one signal per distinct {a,b} episode.
type cycleTracker struct {
	// ring holds the last cycleWindow batch keys. Fixed size, so the buffer
	// can never grow past cycleWindow.
	ring [cycleWindow]string
	// pos is the next write index; once full it also points at the oldest
	// entry, so the chronological window is ring[pos], ring[pos+1], …
	pos int
	// n counts writes so far, saturating at cycleWindow. The window is only
	// eligible for a signal once n == cycleWindow.
	n int
	// episode is the active oscillation's unordered pair key ("min\x00max").
	// Empty means no episode is open. Dedupes re-emits within one continuous
	// episode; cleared the moment the pattern breaks so a later episode of the
	// same pair signals anew.
	episode string
}

func newCycleTracker() *cycleTracker { return &cycleTracker{} }

// ObserveStep records one step's tool-call batch and reports whether this step
// closed a fresh ABAB episode. Returns true at most once per distinct pair.
func (t *cycleTracker) ObserveStep(uses []types.ToolUse) bool {
	return t.observeKey(batchKey(uses))
}

// observeKey is ObserveStep at the batch-key level, exposed so tests and
// callers that already hold a key can drive the tracker directly.
func (t *cycleTracker) observeKey(key string) bool {
	t.ring[t.pos] = key
	t.pos = (t.pos + 1) % cycleWindow
	if t.n < cycleWindow {
		t.n++
	}
	if t.n < cycleWindow {
		return false
	}
	w := t.window()
	a, b := w[0], w[1]
	// require [a,b,a,b] with a != b (sorted so the swapped window [b,a,b,a]
	// of the same ongoing oscillation dedupes to the same episode).
	if a == b || w[2] != a || w[3] != b {
		t.episode = ""
		return false
	}
	ep := episodeKey(a, b)
	if t.episode == ep {
		return false
	}
	t.episode = ep
	return true
}

// window returns the ring contents oldest→newest. Only meaningful once the
// buffer is full (n == cycleWindow).
func (t *cycleTracker) window() [cycleWindow]string {
	var w [cycleWindow]string
	for i := 0; i < cycleWindow; i++ {
		w[i] = t.ring[(t.pos+i)%cycleWindow]
	}
	return w
}

// episodeKey identifies an oscillation episode by its unordered pair, so the
// forward window [a,b,a,b] and its phase-shifted continuation [b,a,b,a] map to
// one episode and a continuous oscillation does not re-emit.
func episodeKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}