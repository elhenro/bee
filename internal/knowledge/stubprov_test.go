package knowledge

import (
	"context"

	"github.com/elhenro/bee/internal/llm"
)

// stubKnowledgeProv is a minimal Provider used by query_test for ExtractTags.
type stubKnowledgeProv struct {
	resp    string
	lastReq llm.Request
}

func (s *stubKnowledgeProv) Name() string { return "stub" }

func (s *stubKnowledgeProv) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	s.lastReq = req
	ch := make(chan llm.Event, 2)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: s.resp}
		ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	}()
	return ch, nil
}
