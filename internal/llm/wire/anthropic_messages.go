// anthropic_messages.go covers Anthropic's native Messages API
// (POST /v1/messages, SSE streaming). Distinct shape vs OpenAI/Chat:
// system is a top-level field (not a message), user/assistant alternate
// strictly, tool calls live as tool_use content blocks in assistant turns,
// and tool results live as tool_result blocks in user turns.
//
// Auth is API-key only (x-api-key). The OAuth subscription path and its
// first-party-client identity headers and tool-name impersonation are
// deliberately not implemented. bee uses the public Anthropic API on its
// own terms. Ephemeral prompt-cache breakpoints are emitted on the system
// block and the last tool entry to reduce token cost on repeat prefixes.
package wire

import (
	"maps"
	"slices"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// BuildAnthropicMessagesRequest converts the internal Request shape into the
// Messages-API body. API-key only — no identity header injection, no tool
// renaming. Ephemeral cache breakpoints go on the static system prefix, the
// last tool and the tail of the history.
func BuildAnthropicMessagesRequest(model, system, systemDynamic string, messages []types.Message, tools []ToolAdvert, maxTokens int, temperature float64, stream bool, thinkingBudget int) AnthropicMessagesRequest {
	req := AnthropicMessagesRequest{
		Model:     model,
		MaxTokens: anthropicMaxTokens(model, maxTokens),
		Stream:    stream,
	}
	cached := func() *AnthropicCacheCtl { return &AnthropicCacheCtl{Type: "ephemeral"} }
	if system != "" {
		// Split off the volatile memory tail (systemDynamic) so the cache
		// breakpoint sits on the STATIC prefix — it then stays cached even when
		// memory changes turn over turn. When systemDynamic isn't a suffix
		// (empty, or a Qwen3 hint was appended after it), fall back to caching
		// the whole system as one block — byte-identical to prior behavior.
		if static, ok := splitDynamic(system, systemDynamic); ok {
			req.System = []AnthropicSysBlock{
				{Type: "text", Text: static, CacheControl: cached()},
				{Type: "text", Text: systemDynamic},
			}
		} else {
			req.System = []AnthropicSysBlock{{Type: "text", Text: system, CacheControl: cached()}}
		}
	}

	if thinkingBudget > 0 {
		if isAdaptiveThinkingModel(model) {
			req.Thinking = &AnthropicThinking{Type: "adaptive"}
		} else if b := clampThinkingBudget(thinkingBudget, req.MaxTokens); b > 0 {
			req.Thinking = &AnthropicThinking{Type: "enabled", BudgetTokens: b}
		}
	}
	// Anthropic rejects requests that set both temperature and thinking.
	if temperature != 0 && req.Thinking == nil {
		req.Temperature = new(temperature)
	}

	for _, t := range tools {
		req.Tools = append(req.Tools, AnthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: normalizeInputSchema(t.Schema),
		})
	}
	// cache breakpoint on the last tool covers the whole tools block
	if n := len(req.Tools); n > 0 {
		req.Tools[n-1].CacheControl = cached()
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
	// Roll prompt-cache breakpoints across the conversation tail so the growing
	// history caches turn-over-turn — not just the static system + tools prefix.
	// Anthropic allows 4 breakpoints; system + last tool take 2, leaving 2 for
	// history here, staying within the limit.
	markHistoryCacheBreakpoints(req.Messages)
	return req
}

// markHistoryCacheBreakpoints sets cache_control on the last content block of
// the final two messages, giving Anthropic rolling prompt-cache breakpoints over
// the conversation tail. Each turn writes a small delta to cache and reads the
// prior conversation back as a cache hit. Sub-minimum prefixes are ignored by
// the API, so marking short early conversations is harmless. No-op when empty.
func markHistoryCacheBreakpoints(msgs []AnthropicMessage) {
	marked := 0
	for i := len(msgs) - 1; i >= 0 && marked < 2; i-- {
		parts := msgs[i].Content
		if len(parts) == 0 {
			continue
		}
		parts[len(parts)-1].CacheControl = &AnthropicCacheCtl{Type: "ephemeral"}
		marked++
	}
}

// normalizeInputSchema ensures the schema Anthropic sees has a top-level
// "type":"object" + "properties" / "required" so the API doesn't 400. Schemas
// declared with only properties (no type) are common in bee tools.
func normalizeInputSchema(s map[string]any) map[string]any {
	out := make(map[string]any, len(s)+2)
	maps.Copy(out, s)
	if _, ok := out["type"]; !ok {
		out["type"] = "object"
	}
	if _, ok := out["properties"]; !ok {
		out["properties"] = map[string]any{}
	}
	return out
}

// minThinkingBudget is the smallest budget_tokens the API accepts.
const minThinkingBudget = 1024

// clampThinkingBudget keeps budget_tokens strictly below max_tokens with room
// left for the visible answer (the API 400s otherwise). Returns 0 when max
// leaves less than the minimum budget, which means thinking stays off.
func clampThinkingBudget(budget, maxTokens int) int {
	if limit := maxTokens * 3 / 4; budget > limit {
		budget = limit
	}
	if budget < minThinkingBudget {
		return 0
	}
	return budget
}

// anthropicMaxTokens picks a default when caller didn't pin one. Anthropic
// requires the field and rejects values above the model's output limit, so
// the old 3.x families keep their small caps while every other claude family
// (including ids newer than this table) gets a generous 16k: thinking shares
// the budget, and a lower value truncates tool-call JSON mid-object. Ids that
// are not claude families (third-party wire clones) stay at 4096.
func anthropicMaxTokens(model string, n int) int {
	if n > 0 {
		return n
	}
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude-3-opus"), strings.Contains(m, "claude-3-haiku"):
		return 4096
	case strings.Contains(m, "claude-3-5"), strings.Contains(m, "claude-3.5"):
		return 8192
	case strings.Contains(m, "opus"), strings.Contains(m, "sonnet"), strings.Contains(m, "haiku"):
		return 16384
	}
	return 4096
}

// isAdaptiveThinkingModel reports whether the model supports type:"adaptive"
// thinking (model decides the budget). Opus 4.6+ / Sonnet 4.6+ require this;
// older thinking models still need type:"enabled" with budget_tokens. The 5
// families are future-proofing.
func isAdaptiveThinkingModel(model string) bool {
	m := strings.ToLower(model)
	return slices.ContainsFunc([]string{"sonnet-4-6", "opus-4-6", "opus-4-7", "opus-5", "sonnet-5"}, func(s string) bool {
		return strings.Contains(m, s)
	})
}

// translateAnthropicMessage maps one internal Message to one Anthropic
// message. RoleSystem is dropped (handled by top-level System field). RoleTool
// is folded into a user turn (Anthropic represents tool results as user
// messages with tool_result blocks).
func translateAnthropicMessage(m types.Message) (AnthropicMessage, bool) {
	var am AnthropicMessage
	switch m.Role {
	case types.RoleUser, types.RoleTool:
		am = AnthropicMessage{Role: "user", Content: translateAnthropicUserBlocks(m.Content)}
	case types.RoleAssistant:
		am = AnthropicMessage{Role: "assistant", Content: translateAnthropicAssistantBlocks(m.Content)}
	}
	if len(am.Content) == 0 {
		return AnthropicMessage{}, false
	}
	return am, true
}

func translateAnthropicUserBlocks(blocks []types.ContentBlock) []AnthropicContentPart {
	out := make([]AnthropicContentPart, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			out = append(out, AnthropicContentPart{Type: "text", Text: b.Text})
		case types.BlockImage:
			if len(b.Data) == 0 {
				continue
			}
			out = append(out, AnthropicContentPart{
				Type: "image",
				Source: &AnthropicImageSource{
					Type:      "base64",
					MediaType: imageMediaType(b),
					Data:      imageBase64(b),
				},
			})
		case types.BlockToolResult:
			if b.Result == nil {
				continue
			}
			// empty text blocks are rejected, and omitempty would drop the text
			// field entirely; same placeholder the responses wire uses
			text := b.Result.Content
			if strings.TrimSpace(text) == "" {
				text = "(no output)"
			}
			out = append(out, AnthropicContentPart{
				Type:      "tool_result",
				ToolUseID: b.Result.UseID,
				IsError:   b.Result.IsError,
				Content: []AnthropicContentPart{
					{Type: "text", Text: text},
				},
			})
		}
	}
	return out
}

func translateAnthropicAssistantBlocks(blocks []types.ContentBlock) []AnthropicContentPart {
	out := make([]AnthropicContentPart, 0, len(blocks))
	var thinking []AnthropicContentPart
	for _, b := range blocks {
		switch b.Type {
		case types.BlockThinking:
			// the API rejects thinking blocks without their signature, so
			// unsigned ones (other providers, old sessions) are left out
			if b.Signature == "" {
				continue
			}
			text := b.Text
			thinking = append(thinking, AnthropicContentPart{Type: "thinking", Thinking: &text, Signature: b.Signature})
		case types.BlockText:
			if strings.TrimSpace(b.Text) == "" {
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
	// thinking must lead the turn
	return append(thinking, out...)
}
