package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/llm/wire"
	"github.com/elhenro/bee/internal/types"
)

// nonStreamLoop drains a single JSON body and emits the equivalent events.
func (p *OpenAICompatProvider) nonStreamLoop(resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	var body struct {
		Choices []struct {
			Message struct {
				Content   string             `json:"content"`
				ToolCalls []wire.ToolCall    `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *wire.StreamUsage `json:"usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		out <- Event{Type: EventError, Err: fmt.Errorf("decode non-stream body: %w", err)}
		return
	}
	if len(body.Choices) == 0 {
		out <- Event{Type: EventDone, StopReason: "stop"}
		return
	}
	choice := body.Choices[0]
	if choice.Message.Content != "" {
		out <- Event{Type: EventTextDelta, Delta: choice.Message.Content}
	}
	for _, tc := range choice.Message.ToolCalls {
		name := wire.SanitizeToolName(tc.Function.Name)
		if name == "" {
			name = tc.Function.Name
		}
		input := map[string]any{}
		var parseErr string
		if tc.Function.Arguments != "" {
			scrubbed := wire.StripMarkupBytes([]byte(tc.Function.Arguments))
			if err := json.Unmarshal(scrubbed, &input); err != nil {
				parseErr = err.Error()
				input = map[string]any{}
			}
		}
		if parseErr != "" {
			input = map[string]any{"_parse_error": parseErr, "_raw_args": tc.Function.Arguments}
		} else {
			wire.StripMarkupInValues(input)
		}
		id := tc.ID
		if id == "" {
			id = "call_" + uuid.NewString()
		}
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: id, Name: name, Input: input,
		}}
	}
	done := Event{Type: EventDone, StopReason: choice.FinishReason}
	if body.Usage != nil {
		done.Usage = &Usage{InputTokens: body.Usage.PromptTokens, OutputTokens: body.Usage.CompletionTokens}
	}
	out <- done
}

// streamLoop parses an SSE stream, threading tool-call deltas through an
// accumulator and emitting per-chunk events.
func (p *OpenAICompatProvider) streamLoop(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer resp.Body.Close()
	defer close(out)

	acc := wire.NewToolCallAccumulator()
	stopReason := ""
	var usage *wire.StreamUsage

	// inactivity watchdog: cancels the request when no SSE line arrives for
	// the configured stall window. Without this, scanner.Scan() blocks
	// indefinitely on a live-but-quiet connection while the TUI loader spins.
	stallWindow := p.cfg.StallTimeout
	if stallWindow == 0 {
		stallWindow = streamStallTimeout
	}
	bumpActivity, stalled, cancelWatchdog := streamWatchdogWith(ctx, resp.Body, stallWindow)
	defer cancelWatchdog()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		case <-stalled:
			out <- Event{Type: EventError, Err: fmt.Errorf("provider %s sse stalled: no data for %s (try a different model)", p.cfg.Name, stallWindow)}
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
		payload := bytes.TrimPrefix(line, []byte("data:"))

		chunk, done, err := wire.ParseChunk(payload)
		if err != nil {
			out <- Event{Type: EventError, Err: err}
			return
		}
		if done {
			break
		}
		if chunk == nil {
			continue
		}

		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		// Bump watchdog only when real output arrives (content/tool-calls/finish).
		// Thinking-only deltas (reasoning_content / reasoning) do NOT reset the timer.
		hasRealOutput := false
		for _, ch := range chunk.Choices {
			// reasoning_content / reasoning arrive on the delta for DeepSeek-
			// reasoner and OpenAI-compat reasoning models. Surface as a
			// separate event so the renderer can show thoughts grayed
			// without mixing them into assistant content.
			if ch.Delta.ReasoningContent != "" {
				out <- Event{Type: EventThinkingDelta, Delta: ch.Delta.ReasoningContent}
			}
			if ch.Delta.Reasoning != "" {
				out <- Event{Type: EventThinkingDelta, Delta: ch.Delta.Reasoning}
			}
			if ch.Delta.Content != "" {
				hasRealOutput = true
				out <- Event{Type: EventTextDelta, Delta: ch.Delta.Content}
			}
			if len(ch.Delta.ToolCalls) > 0 {
				hasRealOutput = true
				acc.Apply(ch.Delta.ToolCalls)
			}
			if ch.FinishReason != nil && *ch.FinishReason != "" {
				hasRealOutput = true
				stopReason = *ch.FinishReason
			}
		}
		if hasRealOutput {
			bumpActivity()
		}
	}

	if err := scanner.Err(); err != nil {
		// surface ctx cancellation cleanly
		if ctx.Err() != nil {
			out <- Event{Type: EventError, Err: ctx.Err()}
			return
		}
		out <- Event{Type: EventError, Err: fmt.Errorf("sse scan: %w", err)}
		return
	}

	calls, err := acc.Finalize()
	if err != nil {
		out <- Event{Type: EventError, Err: err}
		return
	}
	for _, c := range calls {
		id := c.ID
		if id == "" {
			id = "call_" + uuid.NewString()
		}
		input := c.Input
		// parse failure: stop silently swallowing — pass a structured error
		// down so the tool layer surfaces a real diagnostic to the model on
		// the next turn instead of acting on empty input.
		if c.ParseError != "" && len(input) == 0 {
			input = map[string]any{"_parse_error": c.ParseError, "_raw_args": c.RawArgs}
		}
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID: id, Name: c.Name, Input: input,
		}}
	}
	final := Event{Type: EventDone, StopReason: stopReason}
	if usage != nil {
		final.Usage = &Usage{InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens}
	}
	out <- final
}
