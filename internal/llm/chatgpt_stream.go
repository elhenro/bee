package llm

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
)

// responsesUsage maps the wire usage block to the internal Usage.
func responsesUsage(u *wire.ResponsesUsage) *Usage {
	if u == nil {
		return nil
	}
	out := &Usage{InputTokens: u.InputTokens, OutputTokens: u.OutputTokens}
	if d := u.InputTokensDetails; d != nil {
		out.CachedTokens = d.CachedTokens
	}
	return out
}

// nonStreamLoop reads a single JSON body and emits the equivalent events.
func (p *ChatGPTProvider) nonStreamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	var body wire.ResponsesEventBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		emit(ctx, out, Event{Type: EventError, Err: fmt.Errorf("decode non-stream body: %w", err)})
		return
	}
	var evs []Event
	for _, item := range body.Output {
		switch item.Type {
		case "message":
			for _, c := range item.Content {
				if c.Type == "output_text" && c.Text != "" {
					evs = append(evs, Event{Type: EventTextDelta, Delta: c.Text})
				}
			}
		case "function_call":
			evs = append(evs, newToolUse(item.CallID, item.Name, decodeToolArgs(item.Arguments)))
		}
	}
	emitAll(ctx, out, append(evs, Event{Type: EventDone, StopReason: body.Status, Usage: responsesUsage(body.Usage)})...)
}

// respCall is one function call being assembled from stream events.
type respCall struct {
	callID, name string
	args         strings.Builder
}

// fill takes call id and name from item, keeping what is set.
func (c *respCall) fill(item *wire.ResponsesOutputItem) {
	c.callID = cmp.Or(c.callID, item.CallID)
	c.name = cmp.Or(c.name, item.Name)
}

// setArgs replaces accumulated arguments with the authoritative final form.
func (c *respCall) setArgs(args string) {
	if args != "" {
		c.args.Reset()
		c.args.WriteString(args)
	}
}

// respStream accumulates a Responses SSE stream. tool-call arguments arrive as
// deltas keyed by item id (also call id for function_call items); each call
// goes out as one tool_use once the stream ends.
type respStream struct {
	name  string
	calls map[string]*respCall
	order []string
	usage *wire.ResponsesUsage
	stop  string
}

// slot returns the pending call for id, creating it when create is set.
func (s *respStream) slot(id string, create bool) *respCall {
	c, ok := s.calls[id]
	if !ok && create {
		c = &respCall{}
		s.calls[id] = c
		s.order = append(s.order, id)
	}
	return c
}

func (s *respStream) handle(ctx context.Context, out chan<- Event, payload []byte) error {
	ev, err := wire.ParseResponsesEvent(payload)
	if err != nil || ev == nil {
		return err
	}
	item := ev.Item
	isCall := item != nil && item.Type == "function_call"
	switch ev.Type {
	case "response.output_text.delta":
		if ev.Delta != "" {
			return send(ctx, out, Event{Type: EventTextDelta, Delta: ev.Delta})
		}
	case "response.output_item.added":
		if !isCall {
			break
		}
		if id := cmp.Or(item.ID, item.CallID); id != "" {
			s.slot(id, true).fill(item)
		}
	case "response.function_call_arguments.delta":
		if ev.ItemID != "" {
			s.slot(ev.ItemID, true).args.WriteString(ev.Delta)
		}
	case "response.function_call_arguments.done":
		if c := s.slot(ev.ItemID, false); c != nil {
			c.setArgs(ev.Arguments)
		}
	case "response.output_item.done":
		if !isCall {
			break
		}
		if c := s.slot(cmp.Or(item.ID, item.CallID), false); c != nil {
			c.fill(item)
			c.setArgs(item.Arguments)
		}
	case "response.completed", "response.incomplete":
		if r := ev.Response; r != nil {
			if r.Usage != nil {
				s.usage = r.Usage
			}
			s.stop = cmp.Or(r.Status, s.stop)
			if ev.Type == "response.incomplete" {
				s.stop = "incomplete"
				if d := r.IncompleteDetails; d != nil && d.Reason == "max_output_tokens" {
					s.stop = "length"
				}
			}
		}
	case "error":
		// top-level error event: code/message sit on the event itself
		return fmt.Errorf("provider %s: %s", s.name, cmp.Or(ev.Message, ev.Code, "responses stream error"))
	case "response.failed", "response.error":
		msg := "responses stream failed"
		if r := ev.Response; r != nil && r.Error != nil {
			msg = cmp.Or(r.Error.Message, r.Error.Code)
		}
		return errors.New(msg)
	}
	return nil
}

// streamLoop parses the Responses SSE stream.
func (p *ChatGPTProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	s := &respStream{name: p.cfg.Name, calls: map[string]*respCall{}}
	if !readSSE(ctx, resp, p.cfg.Name, effectiveStallTimeout(), out, func(payload []byte, _ func()) error {
		return s.handle(ctx, out, payload)
	}) {
		return
	}
	for _, id := range s.order {
		c := s.calls[id]
		if !emit(ctx, out, newToolUse(c.callID, c.name, decodeToolArgs(c.args.String()))) {
			return
		}
	}
	emit(ctx, out, Event{Type: EventDone, StopReason: s.stop, Usage: responsesUsage(s.usage)})
}
