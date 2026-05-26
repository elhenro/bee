// Package cost tracks per-turn token usage and dollar cost across providers.
// The Tracker is process-local, thread-safe, and consumed by both the TUI
// status bar (for live session total) and the cost-monitor pane (for the
// historical breakdown view).
package cost

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Price is per-million-token rates for one model.
type Price struct {
	InputPerM  float64
	OutputPerM float64
}

// Event is a single usage record produced after a provider stream finishes.
type Event struct {
	Time     time.Time
	Provider string
	Model    string
	Input    int
	Output   int
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
	p, _ := Lookup(provider, model)
	usd := (float64(in)/1_000_000)*p.InputPerM + (float64(out)/1_000_000)*p.OutputPerM
	ev := Event{
		Time:     time.Now().UTC(),
		Provider: provider,
		Model:    model,
		Input:    in,
		Output:   out,
		USD:      usd,
	}
	t.mu.Lock()
	t.events = append(t.events, ev)
	t.estimatedInput = 0
	t.mu.Unlock()
	return ev
}

// Reset drops every recorded event so a fresh session starts at zero.
// Used by /new and /clear to bring the context-fill indicator back to 0%.
func (t *Tracker) Reset() {
	t.mu.Lock()
	t.events = nil
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
	var s Summary
	for _, e := range t.events {
		s.Calls++
		s.Input += e.Input
		s.Output += e.Output
		s.USD += e.USD
	}
	return s
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
	t.mu.RLock()
	defer t.mu.RUnlock()
	m := map[string]Summary{}
	for _, e := range t.events {
		s := m[e.Model]
		s.Calls++
		s.Input += e.Input
		s.Output += e.Output
		s.USD += e.USD
		m[e.Model] = s
	}
	return m
}

// ByProvider groups events by provider name.
func (t *Tracker) ByProvider() map[string]Summary {
	t.mu.RLock()
	defer t.mu.RUnlock()
	m := map[string]Summary{}
	for _, e := range t.events {
		s := m[e.Provider]
		s.Calls++
		s.Input += e.Input
		s.Output += e.Output
		s.USD += e.USD
		m[e.Provider] = s
	}
	return m
}

// SortedKeys returns map keys sorted alphabetically — convenience for
// deterministic rendering in the TUI pane.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Lookup returns pricing for (provider, model). Tries the most specific key
// first (provider+model), then bare model, then a stripped path-suffix
// (openrouter returns "vendor/model" ids). Returns false when nothing
// matches — caller treats that as zero-cost.
func Lookup(provider, model string) (Price, bool) {
	if p, ok := prices[provider+"|"+model]; ok {
		return p, true
	}
	if p, ok := prices[model]; ok {
		return p, true
	}
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		if p, ok := prices[model[idx+1:]]; ok {
			return p, true
		}
	}
	return Price{}, false
}

// prices is the embedded pricing table (USD per million tokens).
// Numbers reflect public list prices as of early 2026. The TUI marks
// unpriced models with a "—" so the user knows the dollar number isn't
// gospel; override via SetPrice if a vendor changes rates mid-session.
var prices = map[string]Price{
	// OpenAI
	"gpt-4o-mini":         {0.15, 0.60},
	"gpt-4o":              {2.50, 10.00},
	"o1":                  {15.00, 60.00},
	"o1-mini":             {3.00, 12.00},
	"o3-mini":             {1.10, 4.40},
	// Anthropic
	"claude-haiku-4-5":     {1.00, 5.00},
	"claude-sonnet-4-5":    {3.00, 15.00},
	"claude-sonnet-4-6":    {3.00, 15.00},
	"claude-opus-4-7":      {15.00, 75.00},
	// DeepSeek
	"deepseek-v4-flash":    {0.07, 1.10},
	"deepseek-chat":        {0.27, 1.10},
	"deepseek-reasoner":    {0.55, 2.19},
	// Google
	"gemini-2.0-flash":     {0.10, 0.40},
	"gemini-2.5-pro":       {1.25, 10.00},
	"gemini-2.5-flash":     {0.30, 2.50},
	// Groq (Llama variants are routed via groq.com)
	"llama-3.3-70b-versatile": {0.59, 0.79},
	"llama-3.1-8b-instant":    {0.05, 0.08},
	// Moonshot Kimi K2 — public list rates (USD/M). OpenRouter ids in the
	// `moonshotai/kimi-k2*` family strip to `kimi-k2*` via Lookup's path-
	// suffix fallback, so the bare slugs cover both direct and routed use.
	"kimi-k2":     {0.60, 2.50},
	"kimi-k2.5":   {0.60, 2.50},
	"kimi-k2.6":   {0.60, 2.50},
}

// SetPrice overrides a model's pricing in the active table. Returns the
// previous value so a caller can restore it. Useful for tests and for
// surfacing a /price command later.
func SetPrice(model string, p Price) Price {
	prev := prices[model]
	prices[model] = p
	return prev
}
