package wire

import (
	"encoding/json"
	"fmt"
)

// StreamChunk is one decoded SSE data: payload from an OpenAI chat stream.
type StreamChunk struct {
	ID      string         `json:"id"`
	Choices []StreamChoice `json:"choices"`
	Usage   *StreamUsage   `json:"usage"`
}

// StreamChoice is one alternative completion within a chunk. OpenAI commonly
// streams a single choice.
type StreamChoice struct {
	Index        int          `json:"index"`
	Delta        StreamDelta  `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
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
type StreamUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ParseChunk decodes one SSE `data:` JSON line. The literal "[DONE]" marker
// returns (nil, true, nil) — caller should treat that as the terminator.
func ParseChunk(data []byte) (*StreamChunk, bool, error) {
	trimmed := trimSpace(data)
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

func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}

// ToolCallAccumulator threads partial tool-call deltas across chunks. Each
// streamed tool call has a stable Index; ID/Name arrive first, Arguments
// accumulate as a JSON string fragment.
type ToolCallAccumulator struct {
	slots map[int]*partialCall
	order []int
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

// Apply merges a chunk's tool-call deltas into the accumulator.
func (a *ToolCallAccumulator) Apply(deltas []StreamToolCall) {
	for _, d := range deltas {
		slot, ok := a.slots[d.Index]
		if !ok {
			slot = &partialCall{}
			a.slots[d.Index] = slot
			a.order = append(a.order, d.Index)
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
	out := make([]FinalizedCall, 0, len(a.order))
	for _, idx := range a.order {
		p := a.slots[idx]
		name := SanitizeToolName(p.Name)
		rawBytes := StripMarkupBytes(p.Args)
		args := map[string]any{}
		var rawArgs, parseErr string
		if len(rawBytes) > 0 {
			if err := json.Unmarshal(rawBytes, &args); err != nil {
				repaired, ok := repairToolArgs(rawBytes)
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
		out = append(out, FinalizedCall{
			ID:         p.ID,
			Name:       name,
			Input:      args,
			RawArgs:    rawArgs,
			ParseError: parseErr,
		})
	}
	return out, nil
}

