package mockprov

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// ScriptedProvider replays scenarios from a fixture file. Implements
// llm.Provider. Safe for concurrent Stream calls (cursor is mutex-guarded)
// but designed for serial use — every Stream advances the cursor to the
// next scenario whose Matcher accepts the request.
type ScriptedProvider struct {
	file *File

	mu     sync.Mutex
	cursor int
}

// NewScripted builds a ScriptedProvider over a parsed fixture.
func NewScripted(f *File) *ScriptedProvider {
	return &ScriptedProvider{file: f}
}

// Name reports the provider identity.
func (s *ScriptedProvider) Name() string { return "mockprov" }

// Stream finds the next un-consumed scenario whose matcher accepts req, then
// emits its events on the returned channel. If no remaining scenario
// accepts, returns an error (fail-fast — tests want regressions visible).
func (s *ScriptedProvider) Stream(ctx context.Context, req llm.Request) (<-chan llm.Event, error) {
	sc, err := s.next(req)
	if err != nil {
		return nil, err
	}

	ch := make(chan llm.Event, len(sc.Events)+1)
	go func() {
		defer close(ch)
		for _, ev := range sc.Events {
			select {
			case <-ctx.Done():
				return
			default:
			}
			ch <- translate(ev)
		}
	}()
	return ch, nil
}

// next advances the cursor to the next matching scenario, returns it.
// Unconsumed scenarios may be skipped only when Matcher.Any is set — every
// other matcher MUST accept the current request or it's a hard error.
func (s *ScriptedProvider) next(req llm.Request) (*Scenario, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cursor >= len(s.file.Scenarios) {
		return nil, fmt.Errorf("mockprov: fixture exhausted after %d scenarios (model called once too many)", s.cursor)
	}
	sc := &s.file.Scenarios[s.cursor]
	if !sc.Match.Accepts(req) {
		// build a short request fingerprint for the error
		_, txt, role := trailing(req.Messages)
		return nil, fmt.Errorf("mockprov: scenario[%d] %q matcher rejected request (role=%s text=%q)",
			s.cursor, sc.Name, role, snippet(txt, 80))
	}
	s.cursor++
	return sc, nil
}

// Remaining reports how many scenarios are still un-consumed. Useful for
// tests asserting the fixture ran to completion.
func (s *ScriptedProvider) Remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.file.Scenarios) - s.cursor
}

func translate(ev Event) llm.Event {
	switch ev.Type {
	case "text_delta":
		return llm.Event{Type: llm.EventTextDelta, Delta: ev.Delta}
	case "thinking_delta":
		return llm.Event{Type: llm.EventThinkingDelta, Delta: ev.Delta}
	case "tool_use":
		use := &types.ToolUse{}
		if ev.Tool != nil {
			use.ID = ev.Tool.ID
			if use.ID == "" {
				use.ID = "toolu_" + uuid.NewString()
			}
			use.Name = ev.Tool.Name
			use.Input = ev.Tool.Input
		}
		return llm.Event{Type: llm.EventToolUse, ToolUse: use}
	case "done":
		out := llm.Event{Type: llm.EventDone, StopReason: ev.StopReason}
		if ev.Usage != nil {
			out.Usage = &llm.Usage{InputTokens: ev.Usage.Input, OutputTokens: ev.Usage.Output}
		}
		return out
	case "error":
		return llm.Event{Type: llm.EventError, Err: fmt.Errorf("%s", ev.Delta)}
	}
	return llm.Event{Type: llm.EventError, Err: fmt.Errorf("mockprov: unknown event type %q", ev.Type)}
}

func snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
