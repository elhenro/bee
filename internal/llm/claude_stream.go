package llm

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
	"github.com/elhenro/bee/internal/types"
	"github.com/google/uuid"
)

// streamLoop parses the Messages SSE stream. Tool-call inputs arrive as
// incremental JSON deltas keyed by block index; we accumulate them and emit
// one EventToolUse per tool_use block at content_block_stop time.
func (p *ClaudeProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event, tools []wire.ToolAdvert) {
	defer resp.Body.Close()
	defer close(out)
	_ = tools // reserved for future tool-name validation

	type pending struct {
		id   string
		name string
		args strings.Builder
	}
	// Block index -> pending tool_use. Anthropic numbers blocks sequentially
	// within a single message_stop.
	calls := map[int]*pending{}
	var order []int
	// thinking blocks seen this message and their signatures by block index.
	// Only a lone signed block round-trips: the loop folds all reasoning into
	// one block, which several signatures cannot authenticate.
	thinkBlocks := 0
	thinkSigs := map[int]string{}
	var usage *wire.AnthropicUsage
	stopReason := ""

	bumpActivity, stalled, cancelWatchdog := streamWatchdog(ctx, resp.Body)
	defer cancelWatchdog()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), sseMaxLine)

	for scanner.Scan() {
		bumpActivity()
		select {
		case <-ctx.Done():
			emit(ctx, out, Event{Type: EventError, Err: ctx.Err()})
			return
		case <-stalled:
			emit(ctx, out, Event{Type: EventError, Err: fmt.Errorf("provider %s stream stalled: no data for %s (try a different model)", p.cfg.Name, effectiveStallTimeout())})
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 || line[0] == ':' {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		ev, err := wire.ParseAnthropicEvent(payload)
		if err != nil {
			emit(ctx, out, Event{Type: EventError, Err: err})
			return
		}
		if ev == nil {
			continue
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil && ev.Message.Usage != nil {
				usage = ev.Message.Usage
			}
		case "content_block_start":
			if ev.ContentBlock == nil {
				continue
			}
			if t := ev.ContentBlock.Type; t == "thinking" || t == "redacted_thinking" {
				thinkBlocks++
			}
			if ev.ContentBlock.Type == "tool_use" {
				calls[ev.Index] = &pending{
					id:   ev.ContentBlock.ID,
					name: ev.ContentBlock.Name,
				}
				order = append(order, ev.Index)
			}
		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text != "" && !emit(ctx, out, Event{Type: EventTextDelta, Delta: ev.Delta.Text}) {
					return
				}
			case "input_json_delta":
				if c, ok := calls[ev.Index]; ok {
					c.args.WriteString(ev.Delta.PartialJSON)
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" && !emit(ctx, out, Event{Type: EventThinkingDelta, Delta: ev.Delta.Thinking}) {
					return
				}
			case "signature_delta":
				// thinking blocks are signed so they can round-trip next turn
				thinkSigs[ev.Index] += ev.Delta.Signature
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				stopReason = ev.Delta.StopReason
			}
			if ev.Usage != nil {
				// usage on message_delta is the final tally; merge over the
				// message_start snapshot.
				if usage == nil {
					usage = ev.Usage
				} else {
					usage.OutputTokens = ev.Usage.OutputTokens
					if ev.Usage.InputTokens > 0 {
						usage.InputTokens = ev.Usage.InputTokens
					}
				}
			}
		case "error":
			emit(ctx, out, Event{Type: EventError, Err: fmt.Errorf("anthropic stream error: %s", string(payload))})
			return
		}
	}

	if err := scanner.Err(); err != nil {
		// the watchdog closes the body, which surfaces here as a read error;
		// report the stall instead of the cryptic close
		select {
		case <-stalled:
			emit(ctx, out, Event{Type: EventError, Err: fmt.Errorf("provider %s stream stalled: no data for %s (try a different model)", p.cfg.Name, effectiveStallTimeout())})
			return
		default:
		}
		if ctx.Err() != nil {
			emit(ctx, out, Event{Type: EventError, Err: ctx.Err()})
			return
		}
		emit(ctx, out, Event{Type: EventError, Err: sseScanError(err)})
		return
	}

	if thinkBlocks == 1 && len(thinkSigs) == 1 {
		for _, sig := range thinkSigs {
			if !emit(ctx, out, Event{Type: EventThinkingDelta, Signature: sig}) {
				return
			}
		}
	}

	for _, idx := range order {
		c := calls[idx]
		input := decodeToolArgs(c.args.String())
		id := c.id
		if id == "" {
			id = "call_" + uuid.NewString()
		}
		if !emit(ctx, out, Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: id, Name: c.name, Input: input,
		}}) {
			return
		}
	}

	final := Event{Type: EventDone, StopReason: stopReason}
	if usage != nil {
		// Anthropic reports input_tokens as the NON-cached portion only; the
		// cached prefix lands in cache_read/cache_creation. Fold them into
		// InputTokens so it reflects the true prompt size — matching the
		// openai-compat convention (CachedTokens ⊆ InputTokens). Without this,
		// once prompt caching kicks in input_tokens collapses to the new-turn
		// delta and the auto-compact budget check stops firing, letting context
		// grow until it overflows the window. CachedTokens carries the cheap
		// cache-read portion so cost/budget code can discount it.
		final.Usage = &Usage{
			InputTokens:  usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens,
			OutputTokens: usage.OutputTokens,
			CachedTokens: usage.CacheReadInputTokens,
		}
	}
	emit(ctx, out, final)
}
