package llm

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/elhenro/bee/internal/types"
)

// AnthropicProvider is a stub. The full native implementation is in claude.go.
// Most users should route through OpenAI-compatible paths or use the
// anthropic-messages wire_api config option.
type AnthropicProvider struct {
	apiKeyEnv string
	baseURL   string
}

// NewAnthropic builds a stub provider. apiKeyEnv is the env var for the
// bearer token; baseURL defaults to https://api.anthropic.com/v1 when empty.
func NewAnthropic(apiKeyEnv, baseURL string) *AnthropicProvider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	return &AnthropicProvider{apiKeyEnv: apiKeyEnv, baseURL: baseURL}
}

// Name returns the static provider identifier.
func (p *AnthropicProvider) Name() string { return "anthropic" }

// anthropicThinking is the wire-shape for Anthropic's extended-thinking
// request field. Set only when req.Thinking maps to a positive budget.
type anthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens"`
}

// buildAnthropicBody assembles the messages-API body fragments that depend
// on Request. Exposed (lower-case) so the future native impl + tests share
// one path. Returns the thinking sub-object (nil when off).
func buildAnthropicBody(req Request) *anthropicThinking {
	if b := ThinkingBudget(req.Thinking); b > 0 {
		return &anthropicThinking{Type: "enabled", BudgetTokens: b}
	}
	return nil
}

// Stream is intentionally not implemented on this stub. Use the native
// path in claude.go or configure the provider with wire_api =
// "anthropic-messages" via openai_compat.go.
func (p *AnthropicProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	_ = buildAnthropicBody(req)
	return nil, errors.New("anthropic stub: use claude.go or wire_api=anthropic-messages")
}

// translateAnthropicBlock converts one internal ContentBlock to its Anthropic
// messages-API wire shape. Returns nil for blocks that don't translate (e.g.
// BlockToolUse from a user-role message, which Anthropic doesn't accept).
//
// Used by the future native Stream impl and exposed lowercase so tests can
// pin the schema without going through HTTP.
func translateAnthropicBlock(b types.ContentBlock) map[string]any {
	switch b.Type {
	case types.BlockText:
		return map[string]any{"type": "text", "text": b.Text}
	case types.BlockImage:
		mt := b.MediaType
		if mt == "" {
			mt = "image/png"
		}
		return map[string]any{
			"type": "image",
			"source": map[string]any{
				"type":       "base64",
				"media_type": mt,
				"data":       base64.StdEncoding.EncodeToString(b.Data),
			},
		}
	case types.BlockToolUse:
		if b.Use == nil {
			return nil
		}
		return map[string]any{
			"type":  "tool_use",
			"id":    b.Use.ID,
			"name":  b.Use.Name,
			"input": b.Use.Input,
		}
	case types.BlockToolResult:
		if b.Result == nil {
			return nil
		}
		out := map[string]any{
			"type":        "tool_result",
			"tool_use_id": b.Result.UseID,
			"content":     b.Result.Content,
		}
		if b.Result.IsError {
			out["is_error"] = true
		}
		return out
	}
	return nil
}
