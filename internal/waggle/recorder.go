// Package waggle is bee's self-improving procedure memory.
//
// The forage recorder watches the agent's read-only tool stream; the miner
// detects repeated routes through it; the promoter crystallizes a route into a
// runnable exec-skill (a waggle) that later sessions follow instead of
// re-deriving. The name comes from the waggle dance: how a bee encodes a proven
// foraging route for the hive to repeat.
package waggle

import "sync"

// Call is one recorded tool invocation in the forage log. Args holds the tool's
// input flattened to strings (sorted-key iteration gives a deterministic shape).
// Mutates marks a state-changing tool so the miner can exclude it. OutHash and
// EstTokens feed dedup and yield estimation.
type Call struct {
	Tool      string
	Args      map[string]string
	Mutates   bool
	OutHash   string
	EstTokens int
}

// Recorder is a bounded per-session ring buffer of recent tool calls.
type Recorder struct {
	mu  sync.Mutex
	buf []Call
	cap int
}

// NewRecorder returns a recorder holding at most capacity recent calls. A
// non-positive capacity defaults to 200.
func NewRecorder(capacity int) *Recorder {
	if capacity <= 0 {
		capacity = 200
	}
	return &Recorder{cap: capacity}
}

// Record appends a call, dropping the oldest once over capacity.
func (r *Recorder) Record(c Call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, c)
	if len(r.buf) > r.cap {
		r.buf = append(r.buf[:0:0], r.buf[len(r.buf)-r.cap:]...)
	}
}

// Calls returns a copy of the buffered calls, oldest first.
func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Call, len(r.buf))
	copy(out, r.buf)
	return out
}
