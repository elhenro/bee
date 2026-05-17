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

// nonStreamLoop reads a single JSON body and emits the equivalent events.
func (p *ChatGPTProvider) nonStreamLoop(resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	var body wire.ResponsesEventBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		out <- Event{Type: EventError, Err: fmt.Errorf("decode non-stream body: %w", err)}
		return
	}
	for _, item := range body.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" && c.Text != "" {
					out <- Event{Type: EventTextDelta, Delta: c.Text}
				}
			}
		case "function_call":
			input := map[string]any{}
			if item.Arguments != "" {
				_ = json.Unmarshal([]byte(item.Arguments), &input)
			}
			id := item.CallID
			if id == "" {
				id = "call_" + uuid.NewString()
			}
			out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
				ID: id, Name: item.Name, Input: input,
			}}
		}
	}
	done := Event{Type: EventDone, StopReason: body.Status}
	if body.Usage != nil {
		done.Usage = &Usage{InputTokens: body.Usage.InputTokens, OutputTokens: body.Usage.OutputTokens}
	}
	out <- done
}

// streamLoop parses the Responses SSE stream. Tool-call arguments arrive as
// incremental deltas; accumulate per-call and emit one EventToolUse on done.
func (p *ChatGPTProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	// per-item state keyed by item_id (also call_id for function_call items)
	type pending struct {
		callID string
		name   string
		args   strings.Builder
	}
	calls := map[string]*pending{}
	var order []string
	var usage *wire.ResponsesUsage
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
		// Responses API emits both `event:` and `data:` lines. We only need
		// the data payload.
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		ev, err := wire.ParseResponsesEvent(payload)
		if err != nil {
			out <- Event{Type: EventError, Err: err}
			return
		}
		if ev == nil {
			continue
		}

		switch ev.Type {
		case "response.output_text.delta":
			if ev.Delta != "" {
				out <- Event{Type: EventTextDelta, Delta: ev.Delta}
			}
		case "response.output_item.added":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				id := ev.Item.ID
				if id == "" {
					id = ev.Item.CallID
				}
				if id == "" {
					continue
				}
				if _, ok := calls[id]; !ok {
					calls[id] = &pending{callID: ev.Item.CallID, name: ev.Item.Name}
					order = append(order, id)
				}
				if calls[id].callID == "" {
					calls[id].callID = ev.Item.CallID
				}
				if calls[id].name == "" {
					calls[id].name = ev.Item.Name
				}
			}
		case "response.function_call_arguments.delta":
			id := ev.ItemID
			if id == "" {
				continue
			}
			c, ok := calls[id]
			if !ok {
				c = &pending{}
				calls[id] = c
				order = append(order, id)
			}
			c.args.WriteString(ev.Delta)
		case "response.function_call_arguments.done":
			id := ev.ItemID
			c, ok := calls[id]
			if !ok {
				continue
			}
			if ev.Arguments != "" {
				// authoritative final form; replace accumulator
				c.args.Reset()
				c.args.WriteString(ev.Arguments)
			}
		case "response.output_item.done":
			if ev.Item != nil && ev.Item.Type == "function_call" {
				id := ev.Item.ID
				if id == "" {
					id = ev.Item.CallID
				}
				if c, ok := calls[id]; ok {
					if c.callID == "" {
						c.callID = ev.Item.CallID
					}
					if c.name == "" {
						c.name = ev.Item.Name
					}
					if ev.Item.Arguments != "" {
						c.args.Reset()
						c.args.WriteString(ev.Item.Arguments)
					}
				}
			}
		case "response.completed":
			if ev.Response != nil {
				if ev.Response.Usage != nil {
					usage = ev.Response.Usage
				}
				if ev.Response.Status != "" {
					stopReason = ev.Response.Status
				}
			}
		case "response.failed", "response.error":
			msg := "responses stream failed"
			if ev.Response != nil && ev.Response.Error != nil {
				msg = ev.Response.Error.Message
				if msg == "" {
					msg = ev.Response.Error.Code
				}
			}
			out <- Event{Type: EventError, Err: fmt.Errorf("%s", msg)}
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

	for _, id := range order {
		c := calls[id]
		input := map[string]any{}
		if c.args.Len() > 0 {
			_ = json.Unmarshal([]byte(c.args.String()), &input)
		}
		callID := c.callID
		if callID == "" {
			callID = "call_" + uuid.NewString()
		}
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: callID, Name: c.name, Input: input,
		}}
	}

	final := Event{Type: EventDone, StopReason: stopReason}
	if usage != nil {
		final.Usage = &Usage{InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens}
	}
	out <- final
}
