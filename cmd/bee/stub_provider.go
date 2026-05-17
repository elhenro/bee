package main

import (
	"context"

	"github.com/elhenro/bee/internal/llm"
)

// stubProvider is the in-process Provider used when BEE_TEST_PROVIDER=stub.
// echoes a deterministic message. avoids network in CI smoke.
type stubProvider struct{}

func newStubProvider() *stubProvider { return &stubProvider{} }

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 3)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "stub: "}
		// echo back a short fingerprint of the request so tests can assert
		text := "ok"
		if len(req.Messages) > 0 {
			last := req.Messages[len(req.Messages)-1]
			for _, c := range last.Content {
				if c.Type == "text" {
					text = c.Text
					break
				}
			}
		}
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: text}
		ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	}()
	return ch, nil
}
