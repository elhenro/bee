// anthropic_messages.go covers Anthropic's native Messages API
// (POST /v1/messages, SSE streaming). Distinct shape vs OpenAI/Chat:
// system is a top-level field (not a message), user/assistant alternate
// strictly, tool calls live as tool_use content blocks in assistant turns,
// and tool results live as tool_result blocks in user turns.
//
// Auth is API-key only (x-api-key). The OAuth subscription path and its
// first-party-client identity headers, tool-name impersonation, and
// ephemeral prompt caching are deliberately not implemented — bee uses
// the public Anthropic API on its own terms.
package wire

import (
	"encoding/base64"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// AnthropicMessagesRequest is the request body for POST /v1/messages.
type AnthropicMessagesRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []AnthropicMessage `json:"messages"`
	// System accepts either a string or an array of {type:text,text}. We use
	// the array form so future caching/multi-block work doesn't require a
	// schema change.
	System      []AnthropicSysBlock `json:"system,omitempty"`
	Tools       []AnthropicTool     `json:"tools,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
	Thinking    *AnthropicThinking  `json:"thinking,omitempty"`
}

// AnthropicSysBlock is one entry of the top-level system array.
type AnthropicSysBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// AnthropicThinking is the extended-thinking knob.
// type=enabled with budget_tokens for older thinking models;
// type=adaptive (no budget) for Opus 4.6+ / Sonnet 4.6+ where the model picks.
type AnthropicThinking struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// AnthropicMessage is one user/assistant turn. Content is always the typed
// array form (string shorthand is omitted to keep tool_use round-trips clean).
type AnthropicMessage struct {
	Role    string                 `json:"role"`
	Content []AnthropicContentPart `json:"content"`
}

// AnthropicContentPart is one block inside a message: text | image |
// tool_use | tool_result.
type AnthropicContentPart struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// image
	Source *AnthropicImageSource `json:"source,omitempty"`
	// tool_use
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
	// tool_result
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
	Content   []AnthropicContentPart `json:"content,omitempty"`
}

// AnthropicImageSource is the base64-payload image envelope.
type AnthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// AnthropicTool advertises a function tool.
type AnthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

// BuildAnthropicMessagesRequest converts the internal Request shape into the
// Messages-API body. API-key only — no identity header injection, no tool
// renaming, no ephemeral caching.
func BuildAnthropicMessagesRequest(model, system string, messages []types.Message, tools []ToolAdvert, maxTokens int, temperature float64, stream bool, thinkingBudget int) AnthropicMessagesRequest {
	req := AnthropicMessagesRequest{
		Model:     model,
		MaxTokens: anthropicMaxTokens(model, maxTokens),
		Stream:    stream,
	}
	if system != "" {
		req.System = append(req.System, AnthropicSysBlock{Type: "text", Text: system})
	}

	thinkingActive := false
	if thinkingBudget > 0 {
		if isAdaptiveThinkingModel(model) {
			req.Thinking = &AnthropicThinking{Type: "adaptive"}
		} else {
			req.Thinking = &AnthropicThinking{Type: "enabled", BudgetTokens: thinkingBudget}
		}
		thinkingActive = true
	}
	// Anthropic rejects requests that set both temperature and thinking.
	if temperature != 0 && !thinkingActive {
		t := temperature
		req.Temperature = &t
	}

	for _, t := range tools {
		req.Tools = append(req.Tools, AnthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: normalizeInputSchema(t.Schema),
		})
	}

	// Anthropic enforces strict user/assistant alternation. Merge consecutive
	// same-role messages by concatenating their content arrays.
	for _, m := range messages {
		am, ok := translateAnthropicMessage(m)
		if !ok {
			continue
		}
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == am.Role {
			req.Messages[n-1].Content = append(req.Messages[n-1].Content, am.Content...)
			continue
		}
		req.Messages = append(req.Messages, am)
	}
	return req
}

// normalizeInputSchema ensures the schema Anthropic sees has a top-level
// "type":"object" + "properties" / "required" so the API doesn't 400. Schemas
// declared with only properties (no type) are common in bee tools.
func normalizeInputSchema(s map[string]any) map[string]any {
	if s == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	out := make(map[string]any, len(s)+2)
	for k, v := range s {
		out[k] = v
	}
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	if _, ok := out["properties"]; !ok {
		out["properties"] = map[string]any{}
	}
	return out
}

// anthropicMaxTokens picks a default when caller didn't pin one. Anthropic
// requires the field. Picks model-aware ceilings so Sonnet/Opus aren't
// truncated at 4k while running tool loops.
func anthropicMaxTokens(model string, n int) int {
	if n > 0 {
		return n
	}
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "sonnet-4-6"), strings.Contains(m, "sonnet-4-5"):
		return 16384
	case strings.Contains(m, "opus"):
		return 8192
	case strings.Contains(m, "haiku"):
		return 8192
	}
	return 4096
}

// isAdaptiveThinkingModel reports whether the model supports type:"adaptive"
// thinking (model decides the budget). Opus 4.6+ / Sonnet 4.6+ require this;
// older thinking models still need type:"enabled" with budget_tokens.
func isAdaptiveThinkingModel(model string) bool {
	m := strings.ToLower(model)
	if strings.Contains(m, "sonnet-4-6") {
		return true
	}
	if strings.Contains(m, "opus-4-6") || strings.Contains(m, "opus-4-7") {
		return true
	}
	// Future-proof: opus-5+/sonnet-5+ are adaptive too.
	if strings.Contains(m, "opus-5") || strings.Contains(m, "sonnet-5") {
		return true
	}
	return false
}

// translateAnthropicMessage maps one internal Message to one Anthropic
// message. RoleSystem is dropped (handled by top-level System field). RoleTool
// is folded into a user turn (Anthropic represents tool results as user
// messages with tool_result blocks).
func translateAnthropicMessage(m types.Message) (AnthropicMessage, bool) {
	switch m.Role {
	case types.RoleSystem:
		return AnthropicMessage{}, false
	case types.RoleUser, types.RoleTool:
		parts := translateAnthropicUserBlocks(m.Content)
		if len(parts) == 0 {
			return AnthropicMessage{}, false
		}
		return AnthropicMessage{Role: "user", Content: parts}, true
	case types.RoleAssistant:
		parts := translateAnthropicAssistantBlocks(m.Content)
		if len(parts) == 0 {
			return AnthropicMessage{}, false
		}
		return AnthropicMessage{Role: "assistant", Content: parts}, true
	}
	return AnthropicMessage{}, false
}

func translateAnthropicUserBlocks(blocks []types.ContentBlock) []AnthropicContentPart {
	out := make([]AnthropicContentPart, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			if b.Text == "" {
				continue
			}
			out = append(out, AnthropicContentPart{Type: "text", Text: b.Text})
		case types.BlockImage:
			if len(b.Data) == 0 {
				continue
			}
			mt := b.MediaType
			if mt == "" {
				mt = "image/png"
			}
			out = append(out, AnthropicContentPart{
				Type: "image",
				Source: &AnthropicImageSource{
					Type:      "base64",
					MediaType: mt,
					Data:      base64.StdEncoding.EncodeToString(b.Data),
				},
			})
		case types.BlockToolResult:
			if b.Result == nil {
				continue
			}
			out = append(out, AnthropicContentPart{
				Type:      "tool_result",
				ToolUseID: b.Result.UseID,
				IsError:   b.Result.IsError,
				Content: []AnthropicContentPart{
					{Type: "text", Text: b.Result.Content},
				},
			})
		}
	}
	return out
}

func translateAnthropicAssistantBlocks(blocks []types.ContentBlock) []AnthropicContentPart {
	out := make([]AnthropicContentPart, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			if b.Text == "" {
				continue
			}
			out = append(out, AnthropicContentPart{Type: "text", Text: b.Text})
		case types.BlockToolUse:
			if b.Use == nil {
				continue
			}
			input := b.Use.Input
			if input == nil {
				input = map[string]any{}
			}
			out = append(out, AnthropicContentPart{
				Type:  "tool_use",
				ID:    b.Use.ID,
				Name:  b.Use.Name,
				Input: input,
			})
		}
	}
	return out
}

