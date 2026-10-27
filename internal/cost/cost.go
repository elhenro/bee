// Package cost tracks per-turn token usage and dollar cost across providers.
// The Tracker is process-local, thread-safe, and consumed by both the TUI
// status bar (for live session total) and the cost-monitor pane (for the
// historical breakdown view).
package cost

import (
	"maps"
	"slices"
	"sync"
	"time"
)

// Event is a single usage record produced after a provider stream finishes.
type Event struct {
	Time     time.Time
	Provider string
	Model    string
	Input    int // includes Cached
	Output   int
	Cached   int // cache-read share of Input
	USD      float64
}

// Summary aggregates Events along some dimension.
type Summary struct {
	Calls  int
	Input  int
	Output int
	USD    float64
}

// Tracker is the in-memory store. Append-only; never persisted.
type Tracker struct {
	mu     sync.RWMutex
	events []Event
	total  Summary // running rollup of events, so Total is O(1)
	// estimatedInput overrides LastInput when non-zero so callers can reflect
	// a post-/compact size drop in the context-fill indicator before the next
	// real provider event lands. Cleared by Record and Reset.
	estimatedInput int
}

// New returns an empty tracker.
func New() *Tracker { return &Tracker{} }

// Record computes cost from the active pricing table and appends an event.
// Unknown models fall back to zero-cost so unpriced local models still log
// token totals without polluting the dollar figure.
func (t *Tracker) Record(provider, model string, in, out int) Event {
	return t.RecordCached(provider, model, in, 0, out, 0)
}

// RecordReported is Record for a provider that reports its own spend: a
// positive reportedUSD replaces the table estimate, so routed models missing
// from the pricing table still show a real dollar figure.
func (t *Tracker) RecordReported(provider, model string, in, out int, reportedUSD float64) Event {
	return t.RecordCached(provider, model, in, 0, out, reportedUSD)
}

// RecordCached is RecordReported for a request whose input (in) includes
// cached tokens read from the prompt cache; those bill at the cached rate.
func (t *Tracker) RecordCached(provider, model string, in, cached, out int, reportedUSD float64) Event {
	p, _ := Lookup(provider, model)
	usd := p.usd(model, in, cached, out)
	if reportedUSD > 0 {
		usd = reportedUSD
	}
	ev := Event{
		Time:     time.Now().UTC(),
		Provider: provider,
		Model:    model,
		Input:    in,
		Output:   out,
		Cached:   cached,
		USD:      usd,
	}
	t.mu.Lock()
	t.events = append(t.events, ev)
	t.total.add(in, out, usd)
	t.estimatedInput = 0
	t.mu.Unlock()
	return ev
}

// Reset drops every recorded event so a fresh session starts at zero.
// Used by /new and /clear to bring the context-fill indicator back to 0%.
func (t *Tracker) Reset() {
	t.mu.Lock()
	t.events = nil
	t.total = Summary{}
	t.estimatedInput = 0
	t.mu.Unlock()
}

// SetEstimatedInput stores an override that LastInput returns until the next
// Record call overwrites it. /compact uses this so the context-fill indicator
// drops to the post-compact estimate immediately, instead of staying frozen
// at the prior turn's input until the next assistant reply.
func (t *Tracker) SetEstimatedInput(n int) {
	t.mu.Lock()
	t.estimatedInput = n
	t.mu.Unlock()
}

// Events returns a snapshot copy of every recorded event.
func (t *Tracker) Events() []Event {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]Event, len(t.events))
	copy(out, t.events)
	return out
}

// LastInput returns the InputTokens of the most recent event, or 0 when the
// tracker is empty. Used by the TUI to estimate current context fill — each
// turn re-sends the full conversation so the latest input count approximates
// live context usage.
func (t *Tracker) LastInput() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.estimatedInput > 0 {
		return t.estimatedInput
	}
	if len(t.events) == 0 {
		return 0
	}
	return t.events[len(t.events)-1].Input
}

// Total returns the rollup of every recorded event.
func (t *Tracker) Total() Summary {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.total
}

func (s *Summary) add(in, out int, usd float64) {
	s.Calls++
	s.Input += in
	s.Output += out
	s.USD += usd
}

// Filter narrows events by provider and/or model. Empty string = no filter
// on that dimension.
func (t *Tracker) Filter(provider, model string) []Event {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var out []Event
	for _, e := range t.events {
		if provider != "" && e.Provider != provider {
			continue
		}
		if model != "" && e.Model != model {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ByModel groups events by model id.
func (t *Tracker) ByModel() map[string]Summary {
	return t.group(func(e Event) string { return e.Model })
}

// ByProvider groups events by provider name.
func (t *Tracker) ByProvider() map[string]Summary {
	return t.group(func(e Event) string { return e.Provider })
}

func (t *Tracker) group(key func(Event) string) map[string]Summary {
	t.mu.RLock()
	defer t.mu.RUnlock()
	m := map[string]Summary{}
	for _, e := range t.events {
		addTo(m, key(e), e.Input, e.Output, e.USD)
	}
	return m
}

func addTo(m map[string]Summary, key string, in, out int, usd float64) {
	s := m[key]
	s.add(in, out, usd)
	m[key] = s
}

// SortedKeys returns map keys sorted alphabetically — convenience for
// deterministic rendering in the TUI pane.
func SortedKeys[V any](m map[string]V) []string { return slices.Sorted(maps.Keys(m)) }
