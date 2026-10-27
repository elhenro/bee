package llm

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/elhenro/bee/internal/llm/wire"
)

// nonStreamLoop drains a single JSON body and emits the equivalent events.
func (p *OpenAICompatProvider) nonStreamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	var body struct {
		Choices []struct {
			Message struct {
				Content   string          `json:"content"`
				ToolCalls []wire.ToolCall `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *wire.StreamUsage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		emit(ctx, out, Event{Type: EventError, Err: fmt.Errorf("decode non-stream body: %w", err)})
		return
	}
	if len(body.Choices) == 0 {
		emit(ctx, out, Event{Type: EventDone, StopReason: "stop"})
		return
	}
	choice := body.Choices[0]
	var evs []Event
	if choice.Message.Content != "" {
		evs = append(evs, Event{Type: EventTextDelta, Delta: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		name := cmp.Or(wire.SanitizeToolName(tc.Function.Name), tc.Function.Name)
		input := map[string]any{}
		if args := tc.Function.Arguments; args != "" {
			if err := json.Unmarshal(wire.StripMarkupBytes([]byte(args)), &input); err != nil {
				input = map[string]any{"_parse_error": err.Error(), "_raw_args": args}
			} else {
				wire.StripMarkupInValues(input)
			}
		}
		evs = append(evs, newToolUse(tc.ID, name, input))
	}
	emitAll(ctx, out, append(evs, Event{Type: EventDone, StopReason: choice.FinishReason, Usage: usageFromWire(body.Usage)})...)
}

// compatStream accumulates a chat-completions SSE stream: tool-call deltas
// thread through an accumulator, text and reasoning go out per chunk.
type compatStream struct {
	name    string
	acc     *wire.ToolCallAccumulator
	stop    string
	sawDone bool
	usage   *wire.StreamUsage
}

func (s *compatStream) handle(ctx context.Context, out chan<- Event, payload []byte, bump func()) error {
	chunk, done, err := wire.ParseChunk(payload)
	if err != nil {
		return err
	}
	if done {
		s.sawDone = true
		return errSSEDone
	}
	if chunk == nil {
		return nil
	}
	// in-band error after a 200 OK (context overflow / backend failure). turn
	// the silent empty completion into a real diagnostic the model can see.
	if chunk.Error != nil && chunk.Error.Message != "" {
		return fmt.Errorf("provider %s stream error: %s", s.name, chunk.Error.Message)
	}
	if chunk.Usage != nil {
		s.usage = chunk.Usage
	}
	// content, tool calls and finish count as liveness after the chunk is
	// handled, so time spent blocked on a slow consumer is not stall time.
	live := false
	for _, ch := range chunk.Choices {
		// reasoning_content / reasoning arrive on the delta for reasoning
		// models. Surface as a separate event so the renderer can show
		// thoughts grayed without mixing them into assistant content. They
		// are liveness too, so a long thinking phase before content does not
		// trip the stall timeout.
		for _, thought := range []string{ch.Delta.ReasoningContent, ch.Delta.Reasoning} {
			if thought == "" {
				continue
			}
			bump()
			if err := send(ctx, out, Event{Type: EventThinkingDelta, Delta: thought}); err != nil {
				return err
			}
		}
		if ch.Delta.Content != "" {
			live = true
			if err := send(ctx, out, Event{Type: EventTextDelta, Delta: ch.Delta.Content}); err != nil {
				return err
			}
		}
		if len(ch.Delta.ToolCalls) > 0 {
			live = true
			s.acc.Apply(ch.Delta.ToolCalls)
			// a call whose arguments are complete while a sibling is still
			// streaming goes out now, so the loop can start running it.
			for _, c := range s.acc.TakeSealed() {
				if err := send(ctx, out, toolUseEvent(c)); err != nil {
					return err
				}
			}
		}
		if ch.FinishReason != nil && *ch.FinishReason != "" {
			live = true
			s.stop = *ch.FinishReason
		}
	}
	if live {
		bump()
	}
	return nil
}

// streamLoop parses an SSE stream, threading tool-call deltas through an
// accumulator and emitting per-chunk events.
func (p *OpenAICompatProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	s := &compatStream{name: p.cfg.Name, acc: wire.NewToolCallAccumulator()}
	// inactivity watchdog: cancels the request when no SSE line arrives for
	// the configured stall window. Without this, a read blocks indefinitely on
	// a live-but-quiet connection while the TUI loader spins.
	stall := effectiveStallTimeout()
	if !readSSE(ctx, resp, p.cfg.Name, stall, out, func(payload []byte, bump func()) error {
		return s.handle(ctx, out, payload, bump)
	}) {
		return
	}

	if s.stop == "length" || (s.stop == "" && !s.sawDone) {
		s.acc.MarkTruncated()
	}
	calls, err := s.acc.Finalize()
	if err != nil {
		emit(ctx, out, Event{Type: EventError, Err: err})
		return
	}
	for _, c := range calls {
		if !emit(ctx, out, toolUseEvent(c)) {
			return
		}
	}
	emit(ctx, out, Event{Type: EventDone, StopReason: s.stop, Usage: usageFromWire(s.usage)})
}

// usageFromWire maps the wire usage block to the internal Usage, carrying the
// optional cost and cached-token fields when present.
func usageFromWire(u *wire.StreamUsage) *Usage {
	if u == nil {
		return nil
	}
	out := &Usage{InputTokens: u.PromptTokens, OutputTokens: u.CompletionTokens, CostUSD: u.Cost}
	if u.PromptTokensDetails != nil {
		out.CachedTokens = u.PromptTokensDetails.CachedTokens
	}
	return out
}

// toolUseEvent turns an assembled call into its stream event.
func toolUseEvent(c wire.FinalizedCall) Event {
	input := c.Input
	// parse failure: stop silently swallowing — pass a structured error
	// down so the tool layer surfaces a real diagnostic to the model on
	// the next turn instead of acting on empty input.
	if c.ParseError != "" && len(input) == 0 {
		input = map[string]any{"_parse_error": c.ParseError, "_raw_args": c.RawArgs}
	}
	return newToolUse(c.ID, c.Name, input)
}
