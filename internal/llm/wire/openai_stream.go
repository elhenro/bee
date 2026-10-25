package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// StreamChunk is one decoded SSE data: payload from an OpenAI chat stream.
type StreamChunk struct {
	ID      string         `json:"id"`
	Choices []StreamChoice `json:"choices"`
	Usage   *StreamUsage   `json:"usage"`
	// Error carries an in-band error some servers emit after a 200 OK: the SSE
	// opens, then a single `data: {"error":{...}}` lands on context overflow or
	// a mid-stream backend failure. The pre-stream retry loop never sees these
	// (status was 200), so without surfacing it the turn ends as a silent empty
	// completion. Caller converts a non-empty message to an error event.
	Error *StreamError `json:"error,omitempty"`
}

// StreamError is the in-band error object. Code is any since servers send it as
// either a string ("context_length_exceeded") or an int.
type StreamError struct {
	Message string `json:"message"`
	Code    any    `json:"code,omitempty"`
}

// StreamChoice is one alternative completion within a chunk. OpenAI commonly
// streams a single choice.
type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

// StreamDelta carries the incremental fields for this chunk.
//
// ReasoningContent is DeepSeek-reasoner's chain-of-thought field. Reasoning
// is OpenAI o-series' equivalent (some compat servers expose it on chat-
// completions deltas too). Both are agent-facing — never echoed back as
// assistant content; rendered separately in a dimmed style.
type StreamDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"`
	Reasoning        string           `json:"reasoning,omitempty"`
	ToolCalls        []StreamToolCall `json:"tool_calls,omitempty"`
}

// StreamToolCall is the streamed shape of a tool invocation. Function name +
// args come in pieces over multiple chunks; the index tells us which slot.
type StreamToolCall struct {
	Index    int                 `json:"index"`
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function StreamFunctionDelta `json:"function"`
}

// StreamFunctionDelta is the incremental function payload.
type StreamFunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// StreamUsage is the optional usage block, often only on the final chunk.
//
// Cost is the actual spend in account credits, returned by routed aggregators
// when the request opts in (see ChatRequest.Usage). Absent on strict endpoints,
// so it stays zero there. PromptTokensDetails carries a cached-prompt count
// when reported.
type StreamUsage struct {
	PromptTokens        int     `json:"prompt_tokens"`
	CompletionTokens    int     `json:"completion_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	Cost                float64 `json:"cost"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

// ParseChunk decodes one SSE `data:` JSON line. The literal "[DONE]" marker
// returns (nil, true, nil) — caller should treat that as the terminator.
func ParseChunk(data []byte) (*StreamChunk, bool, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, false, nil
	}
	if string(trimmed) == "[DONE]" {
		return nil, true, nil
	}
	var c StreamChunk
	if err := json.Unmarshal(trimmed, &c); err != nil {
		return nil, false, fmt.Errorf("decode chunk: %w", err)
	}
	return &c, false, nil
}

// ToolCallAccumulator threads partial tool-call deltas across chunks. Each
// streamed tool call has a stable Index; ID/Name arrive first, Arguments
// accumulate as a JSON string fragment.
type ToolCallAccumulator struct {
	slots map[int]*partialCall // current slot per stream index
	order []*partialCall
	// sealed is how many leading calls TakeSealed already handed out; Finalize
	// returns only the rest.
	sealed int
	// truncated marks a stream cut short (length stop, or ended without a
	// finish signal). Finalize then refuses to close open JSON: a half-written
	// argument object would run as if complete.
	truncated bool
}

type partialCall struct {
	ID   string
	Name string
	Args []byte
}

// NewToolCallAccumulator builds a fresh accumulator.
func NewToolCallAccumulator() *ToolCallAccumulator {
	return &ToolCallAccumulator{slots: map[int]*partialCall{}}
}

// MarkTruncated tells Finalize the stream did not end cleanly.
func (a *ToolCallAccumulator) MarkTruncated() { a.truncated = true }

// Apply merges a chunk's tool-call deltas into the accumulator.
func (a *ToolCallAccumulator) Apply(deltas []StreamToolCall) {
	for _, d := range deltas {
		slot, ok := a.slots[d.Index]
		// servers that reuse one index for parallel calls differ by id
		if !ok || (d.ID != "" && slot.ID != "" && d.ID != slot.ID) {
			slot = &partialCall{}
			a.slots[d.Index] = slot
			a.order = append(a.order, slot)
		}
		if d.ID != "" {
			slot.ID = d.ID
		}
		if d.Function.Name != "" {
			slot.Name = d.Function.Name
		}
		if d.Function.Arguments != "" {
			slot.Args = append(slot.Args, d.Function.Arguments...)
		}
	}
}

// FinalizedCall is the assembled tool call after stream end. RawArgs holds
// the un-decoded argument string when Input is empty due to a parse failure
// caller can surface to the model instead of executing with empty args.
// ParseError is the json error message (non-empty only on failure).
type FinalizedCall struct {
	ID         string
	Name       string
	Input      map[string]any
	RawArgs    string
	ParseError string
}

// Finalize returns the assembled tool calls in index order. Empty Args are
// treated as `{}`. Malformed JSON is repaired before failing so a stray
// trailing brace or unbalanced delta from a noisy model doesn't kill the
// whole turn — see repairToolArgs. When repair also fails, the call is
// returned with empty Input + RawArgs/ParseError populated so the caller
// can surface a structured error to the model rather than silently mis-
// executing.
func (a *ToolCallAccumulator) Finalize() ([]FinalizedCall, error) {
	out := make([]FinalizedCall, 0, len(a.order)-a.sealed)
	for _, p := range a.order[a.sealed:] {
		out = append(out, a.finalizeCall(p))
	}
	return out, nil
}

// TakeSealed hands out, in order, the leading calls that are provably complete
// mid-stream: a later call has started and the call's arguments already form a
// valid JSON value, so nothing legitimate can still be appended. This lets the
// loop begin running a call while the model is still writing its siblings. It
// stops at the first call that is not yet provable to keep emission order; the
// rest wait for Finalize.
func (a *ToolCallAccumulator) TakeSealed() []FinalizedCall {
	var out []FinalizedCall
	for a.sealed < len(a.order)-1 {
		p := a.order[a.sealed]
		if p.Name == "" || len(p.Args) == 0 || !json.Valid(p.Args) {
			break
		}
		out = append(out, a.finalizeCall(p))
		a.sealed++
	}
	return out
}

func (a *ToolCallAccumulator) finalizeCall(p *partialCall) FinalizedCall {
	name := SanitizeToolName(p.Name)
	rawBytes := StripMarkupBytes(p.Args)
	args := map[string]any{}
	var rawArgs, parseErr string
	if len(rawBytes) > 0 {
		if err := json.Unmarshal(rawBytes, &args); err != nil {
			repaired, ok := repairToolArgs(rawBytes, !a.truncated)
			if !ok {
				rawArgs = string(p.Args)
				parseErr = fmt.Sprintf("decode tool args for %s: %v (raw=%q)", p.Name, err, truncForErr(p.Args))
				args = map[string]any{}
			} else if err := json.Unmarshal(repaired, &args); err != nil {
				rawArgs = string(p.Args)
				parseErr = fmt.Sprintf("decode tool args for %s after repair: %v (raw=%q)", p.Name, err, truncForErr(p.Args))
				args = map[string]any{}
			}
		}
	}
	StripMarkupInValues(args)
	if name == "" && p.Name != "" {
		// every char in the name was markup/junk. surface as parse error
		// so the loop can return a useful diagnostic to the model.
		if parseErr == "" {
			parseErr = fmt.Sprintf("tool name unrecognizable after stripping model markup (raw=%q)", truncForErr([]byte(p.Name)))
		}
		name = p.Name
	}
	return FinalizedCall{
		ID:         p.ID,
		Name:       name,
		Input:      args,
		RawArgs:    rawArgs,
		ParseError: parseErr,
	}
}
