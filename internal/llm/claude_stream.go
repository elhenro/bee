package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
)

// claudeCall is one tool_use block being assembled from stream events.
type claudeCall struct {
	id, name string
	args     strings.Builder
}

// claudeStream accumulates a Messages SSE stream. Tool-call inputs arrive as
// incremental JSON deltas keyed by block index (blocks are numbered
// sequentially within a message); each call goes out as one tool_use once the
// stream ends.
type claudeStream struct {
	calls map[int]*claudeCall
	order []int
	// thinking blocks seen this message and their signatures by block index.
	// Only a lone signed block round-trips: the loop folds all reasoning into
	// one block, which several signatures cannot authenticate.
	thinkBlocks int
	thinkSigs   map[int]string
	usage       *wire.AnthropicUsage
	stop        string
}

func (s *claudeStream) handle(ctx context.Context, out chan<- Event, payload []byte) error {
	ev, err := wire.ParseAnthropicEvent(payload)
	if err != nil || ev == nil {
		return err
	}
	switch ev.Type {
	case "message_start":
		if ev.Message != nil && ev.Message.Usage != nil {
			s.usage = ev.Message.Usage
		}
	case "content_block_start":
		switch b := ev.ContentBlock; {
		case b == nil:
		case b.Type == "thinking" || b.Type == "redacted_thinking":
			s.thinkBlocks++
		case b.Type == "tool_use":
			s.calls[ev.Index] = &claudeCall{id: b.ID, name: b.Name}
			s.order = append(s.order, ev.Index)
		}
	case "content_block_delta":
		d := ev.Delta
		if d == nil {
			return nil
		}
		switch d.Type {
		case "text_delta":
			if d.Text != "" {
				return send(ctx, out, Event{Type: EventTextDelta, Delta: d.Text})
			}
		case "input_json_delta":
			if c, ok := s.calls[ev.Index]; ok {
				c.args.WriteString(d.PartialJSON)
			}
		case "thinking_delta":
			if d.Thinking != "" {
				return send(ctx, out, Event{Type: EventThinkingDelta, Delta: d.Thinking})
			}
		case "signature_delta":
			// thinking blocks are signed so they can round-trip next turn
			s.thinkSigs[ev.Index] += d.Signature
		}
	case "message_delta":
		if ev.Delta != nil && ev.Delta.StopReason != "" {
			s.stop = ev.Delta.StopReason
		}
		if u := ev.Usage; u != nil {
			// usage on message_delta is the final tally; merge over the
			// message_start snapshot.
			if s.usage == nil {
				s.usage = u
			} else {
				s.usage.OutputTokens = u.OutputTokens
				if u.InputTokens > 0 {
					s.usage.InputTokens = u.InputTokens
				}
			}
		}
	case "error":
		err := fmt.Errorf("anthropic stream error: %s", payload)
		// overload and internal errors arrive in-band after a 200; carry the
		// stream-scan marker so the loop treats them as a retryable hiccup
		if ev.Error.Transient() {
			err = sseScanError(err)
		}
		return err
	}
	return nil
}

// anthropicUsage maps the wire usage block to the internal Usage. Anthropic
// reports input_tokens as the NON-cached portion only; the cached prefix lands
// in cache_read/cache_creation. Fold them into InputTokens so it reflects the
// true prompt size — matching the openai-compat convention (CachedTokens ⊆
// InputTokens). Without this, once prompt caching kicks in input_tokens
// collapses to the new-turn delta and the auto-compact budget check stops
// firing, letting context grow until it overflows the window. CachedTokens
// carries the cheap cache-read portion so cost/budget code can discount it.
func anthropicUsage(u *wire.AnthropicUsage) *Usage {
	if u == nil {
		return nil
	}
	return &Usage{
		InputTokens:  u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens,
		OutputTokens: u.OutputTokens,
		CachedTokens: u.CacheReadInputTokens,
	}
}

// streamLoop parses the Messages SSE stream.
func (p *ClaudeProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	s := &claudeStream{calls: map[int]*claudeCall{}, thinkSigs: map[int]string{}}
	if !readSSE(ctx, resp, p.cfg.Name, effectiveStallTimeout(), out, func(payload []byte, _ func()) error {
		return s.handle(ctx, out, payload)
	}) {
		return
	}

	var evs []Event
	if s.thinkBlocks == 1 && len(s.thinkSigs) == 1 {
		for _, sig := range s.thinkSigs {
			evs = append(evs, Event{Type: EventThinkingDelta, Signature: sig})
		}
	}
	for _, idx := range s.order {
		c := s.calls[idx]
		evs = append(evs, newToolUse(c.id, c.name, decodeToolArgs(c.args.String())))
	}
	emitAll(ctx, out, append(evs, Event{Type: EventDone, StopReason: s.stop, Usage: anthropicUsage(s.usage)})...)
}
