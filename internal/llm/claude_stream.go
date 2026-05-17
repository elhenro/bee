package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/llm/wire"
	"github.com/elhenro/bee/internal/types"
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
	var usage *wire.AnthropicUsage
	stopReason := ""

	bumpActivity, stalled, cancelWatchdog := streamWatchdog(ctx, resp.Body)
	defer cancelWatchdog()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		bumpActivity()
		select {
		case <-ctx.Done():
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		case <-stalled:
			out <- Event{Type: EventError, Err: fmt.Errorf("provider %s stream stalled: no data for %s (try a different model)", p.cfg.Name, streamStallTimeout)}
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
			out <- Event{Type: EventError, Err: err}
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
				if ev.Delta.Text != "" {
					out <- Event{Type: EventTextDelta, Delta: ev.Delta.Text}
				}
			case "input_json_delta":
				if c, ok := calls[ev.Index]; ok {
					c.args.WriteString(ev.Delta.PartialJSON)
				}
			case "thinking_delta":
				if ev.Delta.Thinking != "" {
					out <- Event{Type: EventThinkingDelta, Delta: ev.Delta.Thinking}
				}
			case "signature_delta":
				// Anthropic signs thinking blocks so they can round-trip
				// back to the model next turn. bee currently drops thinking
				// blocks on the wire (types.BlockThinking is display-only),
				// so the signature is parsed and discarded. Kept here so
				// future thinking-roundtrip work has a hook point.
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
			out <- Event{Type: EventError, Err: fmt.Errorf("anthropic stream error: %s", string(payload))}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		}
		out <- Event{Type: EventError, Err: fmt.Errorf("sse scan: %w", err)}
		return
	}

	for _, idx := range order {
		c := calls[idx]
		input := map[string]any{}
		if c.args.Len() > 0 {
			_ = json.Unmarshal([]byte(c.args.String()), &input)
		}
		id := c.id
		if id == "" {
			id = "call_" + uuid.NewString()
		}
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: id, Name: c.name, Input: input,
		}}
	}

	final := Event{Type: EventDone, StopReason: stopReason}
	if usage != nil {
		final.Usage = &Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
	}
	out <- final
}
