// Package wire translates internal agent types to/from provider wire formats.
//
// openai.go covers the OpenAI Chat Completions schema, which OpenRouter,
// DeepSeek, Groq, Ollama, LM Studio, Together, and Fireworks all speak.
package wire

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// SplitCachedSystem renders a system prompt as content parts with a cache
// breakpoint on the STATIC prefix. When dynamic is a non-empty suffix of system,
// the static head is cached and the volatile tail (memory) is a separate
// uncached part, so the prefix stays cached even as memory changes. Otherwise
// the whole system is one cached part.
func SplitCachedSystem(system, dynamic string) []ChatContentPart {
	cached := &OpenAICacheControl{Type: "ephemeral"}
	if static, ok := splitDynamic(system, dynamic); ok {
		return []ChatContentPart{
			{Type: "text", Text: static, CacheControl: cached},
			{Type: "text", Text: dynamic},
		}
	}
	return []ChatContentPart{{Type: "text", Text: system, CacheControl: cached}}
}

// splitDynamic returns the static head of system when dynamic is a non-empty
// proper suffix of it. Otherwise ok is false and the whole system is one
// cacheable block.
func splitDynamic(system, dynamic string) (static string, ok bool) {
	if dynamic == "" || dynamic == system {
		return "", false
	}
	return strings.CutSuffix(system, dynamic)
}

// BuildRequest converts internal Request shape to OpenAI wire form.
func BuildRequest(model string, system string, messages []types.Message, tools []ToolAdvert, maxTokens int, temperature, topP float64, stop []string, stream bool) ChatRequest {
	out := ChatRequest{
		Model:     model,
		Stream:    stream,
		MaxTokens: max(maxTokens, 0),
	}
	if stream {
		out.StreamOptions = &StreamOptions{IncludeUsage: true}
	}
	// only set temperature when non-zero; some providers reject 0.0 explicitly
	if temperature != 0 {
		out.Temperature = new(temperature)
	}
	if topP != 0 {
		out.TopP = new(topP)
	}
	if len(stop) > 0 {
		out.Stop = stop
	}
	if system != "" {
		out.Messages = append(out.Messages, ChatMessage{Role: "system", Content: system})
	}
	for _, m := range messages {
		out.Messages = append(out.Messages, translateMessage(m)...)
	}
	for _, t := range tools {
		out.Tools = append(out.Tools, ChatTool{
			Type: "function",
			Function: ChatFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Schema,
			},
		})
	}
	return out
}

// ToolAdvert mirrors llm.ToolSpec without the import cycle.
type ToolAdvert struct {
	Name        string
	Description string
	Schema      map[string]any
}

// translateMessage flattens an internal Message into 1..N OpenAI ChatMessages.
// tool_result blocks become their own role:"tool" messages; tool_use blocks
// fold into the assistant message's tool_calls.
func translateMessage(m types.Message) []ChatMessage {
	switch m.Role {
	case types.RoleSystem:
		return []ChatMessage{{Role: "system", Content: collectText(m.Content)}}
	case types.RoleUser:
		return splitUser(m.Content)
	case types.RoleAssistant:
		return []ChatMessage{assistantMessage(m.Content)}
	case types.RoleTool:
		// tool role isn't normally used at the top level; treat blocks as user
		// tool_results
		return splitUser(m.Content)
	}
	return nil
}

func splitUser(blocks []types.ContentBlock) []ChatMessage {
	var msgs []ChatMessage
	hasImage := slices.ContainsFunc(blocks, func(b types.ContentBlock) bool { return b.Type == types.BlockImage })
	// vision form: build a typed-parts array (text + image_url entries) so the
	// model sees both modalities. otherwise fall back to plain-string content
	// to keep non-vision provider/model paths unchanged.
	var parts []map[string]any
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			if hasImage {
				parts = append(parts, map[string]any{"type": "text", "text": b.Text})
			}
		case types.BlockImage:
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": imageDataURL(b),
				},
			})
		case types.BlockToolResult:
			if b.Result == nil {
				continue
			}
			msgs = append(msgs, ChatMessage{
				Role:       "tool",
				ToolCallID: b.Result.UseID,
				Content:    toolResultText(b.Result),
			})
		}
	}
	// tool messages must directly follow the assistant tool_calls, so a user
	// message that rides with tool results (pictures a tool produced) goes after
	if hasImage {
		return append(msgs, ChatMessage{Role: "user", Content: parts})
	}
	if text := collectText(blocks); text != "" {
		// prepend, so text precedes tool results in stable order
		msgs = append([]ChatMessage{{Role: "user", Content: text}}, msgs...)
	}
	return msgs
}

func assistantMessage(blocks []types.ContentBlock) ChatMessage {
	msg := ChatMessage{Role: "assistant", Content: collectText(blocks)}
	for _, b := range blocks {
		if b.Type != types.BlockToolUse || b.Use == nil {
			continue
		}
		args, _ := json.Marshal(b.Use.Input)
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID:   b.Use.ID,
			Type: "function",
			Function: FunctionCall{
				Name:      b.Use.Name,
				Arguments: string(args),
			},
		})
	}
	return msg
}

// collectText joins the text blocks with newlines.
func collectText(blocks []types.ContentBlock) string {
	var b strings.Builder
	for _, c := range blocks {
		if c.Type == types.BlockText {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// toolResultText is a tool result as the model reads it: errors are flagged.
func toolResultText(r *types.ToolResult) string {
	if r.IsError {
		return "ERROR: " + r.Content
	}
	return r.Content
}
