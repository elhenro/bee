// Package llm defines the Provider interface and shared event types.
//
// Concrete provider adapters (openai_compat.go, anthropic.go) live alongside
// and translate the internal types in github.com/elhenro/bee/internal/types
// to/from each provider's wire format. The rest of the codebase only depends
// on this interface.
package llm

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// Provider streams a chat completion. The returned channel is closed when the
// turn ends (stop reason emitted) or the context is canceled.
type Provider interface {
	Name() string
	Stream(ctx context.Context, req Request) (<-chan Event, error)
}

// Request is the agent-owned shape of a chat call. Adapters translate to the
// provider's wire format.
type Request struct {
	Model       string
	System      string
	Messages    []types.Message
	Tools       []ToolSpec
	MaxTokens   int
	Temperature float64
	Stream      bool
	// Thinking selects the extended-reasoning budget for providers that
	// support it. Off means omit the field entirely.
	Thinking Thinking
}

// Thinking enumerates the supported extended-reasoning levels. Adapters map
// these to provider-specific fields (Anthropic budget_tokens, OpenAI
// reasoning_effort).
type Thinking string

const (
	// ThinkingAuto = "medium when model supports reasoning, off otherwise".
	// Resolve with ResolveThinking before sending to providers — wire layers
	// only understand off/low/medium/high.
	ThinkingAuto   Thinking = "auto"
	ThinkingOff    Thinking = "off"
	ThinkingLow    Thinking = "low"
	ThinkingMedium Thinking = "medium"
	ThinkingHigh   Thinking = "high"
)

// ThinkingBudget maps a level to a token budget for thinking-enabled providers.
// Off returns 0 → caller should omit the thinking field.
func ThinkingBudget(t Thinking) int {
	switch t {
	case ThinkingLow:
		return 1024
	case ThinkingMedium:
		return 4096
	case ThinkingHigh:
		return 16384
	}
	return 0
}

// ParseThinking accepts "auto"/"off"/"low"/"medium"/"high" (case insensitive)
// and returns the canonical Thinking value. Unknown strings return ThinkingOff.
func ParseThinking(s string) Thinking {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto":
		return ThinkingAuto
	case "low":
		return ThinkingLow
	case "medium", "med":
		return ThinkingMedium
	case "high":
		return ThinkingHigh
	default:
		return ThinkingOff
	}
}

// ResolveThinking turns ThinkingAuto into ThinkingMedium for reasoning-capable
// models and ThinkingOff for everything else. Non-auto values pass through
// unchanged. Call before building a Request — provider adapters only see the
// resolved level, never the sentinel.
func ResolveThinking(t Thinking, modelID string) Thinking {
	if t != ThinkingAuto {
		return t
	}
	if SupportsThinking(modelID) {
		return ThinkingMedium
	}
	return ThinkingOff
}

// ToolSpec is the agent-side advertisement of a tool to the model.
//
// Description goes to the provider's tool-spec (API) — full sentence is fine.
// PromptSnippet is the short prompt-manifest line (≤60 chars).
// When PromptSnippet is empty, the prompt manifest falls back to the first
// line of Description, truncated by the profile's ToolDescChars budget.
type ToolSpec struct {
	Name          string         `json:"name"`
	Description   string         `json:"description"`
	PromptSnippet string         `json:"-"`
	Schema        map[string]any `json:"schema"`
}

// Event is a streamed token, tool call, or terminal signal from a provider.
type Event struct {
	Type     EventType
	Delta    string         // for EventTextDelta
	ToolUse  *types.ToolUse // for EventToolUse
	StopReason string       // for EventDone
	Err      error          // for EventError
	Usage    *Usage         // optional, on EventDone
}

type EventType string

const (
	EventTextDelta     EventType = "text_delta"
	EventThinkingDelta EventType = "thinking_delta"
	EventToolUse       EventType = "tool_use"
	EventDone          EventType = "done"
	EventError         EventType = "error"
)

// Usage captures token accounting reported by the provider.
type Usage struct {
	InputTokens  int
	OutputTokens int
}
