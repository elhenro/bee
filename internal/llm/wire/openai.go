// Package wire translates internal agent types to/from provider wire formats.
//
// openai.go covers the OpenAI Chat Completions schema, which OpenRouter,
// DeepSeek, Groq, Ollama, LM Studio, Together, and Fireworks all speak.
package wire

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// ChatRequest is the OpenAI chat completions body.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ChatTool    `json:"tools,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	// StreamOptions opts in to per-stream extras. Required (include_usage=true)
	// for OpenAI/OpenRouter/DeepSeek to emit token counts on the final SSE
	// chunk; otherwise the stream finishes without a Usage event.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
	// ReasoningEffort is OpenAI's o-series extended-reasoning knob. Values:
	// "low" | "medium" | "high". Omitted when empty.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// StreamOptions matches OpenAI's stream_options envelope. Only include_usage
// is used today.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatMessage is one role-tagged message in the OpenAI schema.
//
// Content is `any` so we can emit either a plain string (text-only) or an
// array of typed parts ({"type":"text",...},{"type":"image_url",...}) for
// vision-capable user messages.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is OpenAI's tool_use envelope.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the function name + JSON-string args.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatTool advertises a tool to the model.
type ChatTool struct {
	Type     string       `json:"type"`
	Function ChatFunction `json:"function"`
}

// ChatFunction is the function-shaped half of a tool spec.
type ChatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// BuildRequest converts internal Request shape to OpenAI wire form.
func BuildRequest(model string, system string, messages []types.Message, tools []ToolAdvert, maxTokens int, temperature float64, stream bool) ChatRequest {
	out := ChatRequest{
		Model:  model,
		Stream: stream,
	}
	if stream {
		out.StreamOptions = &StreamOptions{IncludeUsage: true}
	}
	if maxTokens > 0 {
		out.MaxTokens = maxTokens
	}
	// only set temperature when non-zero; some providers reject 0.0 explicitly
	if temperature != 0 {
		t := temperature
		out.Temperature = &t
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
	var text strings.Builder
	hasImage := false
	for _, b := range blocks {
		if b.Type == types.BlockImage {
			hasImage = true
			break
		}
	}
	// vision form: build a typed-parts array (text + image_url entries) so the
	// model sees both modalities. otherwise fall back to plain-string content
	// to keep non-vision provider/model paths unchanged.
	var parts []map[string]any
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			if hasImage {
				parts = append(parts, map[string]any{"type": "text", "text": b.Text})
			} else {
				if text.Len() > 0 {
					text.WriteString("\n")
				}
				text.WriteString(b.Text)
			}
		case types.BlockImage:
			mt := b.MediaType
			if mt == "" {
				mt = "image/png"
			}
			parts = append(parts, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b.Data),
				},
			})
		case types.BlockToolResult:
			if b.Result == nil {
				continue
			}
			content := b.Result.Content
			if b.Result.IsError {
				content = "ERROR: " + content
			}
			msgs = append(msgs, ChatMessage{
				Role:       "tool",
				ToolCallID: b.Result.UseID,
				Content:    content,
			})
		}
	}
	if hasImage && len(parts) > 0 {
		msgs = append([]ChatMessage{{Role: "user", Content: parts}}, msgs...)
	} else if text.Len() > 0 {
		// prepend, so text precedes tool results in stable order
		msgs = append([]ChatMessage{{Role: "user", Content: text.String()}}, msgs...)
	}
	return msgs
}

func assistantMessage(blocks []types.ContentBlock) ChatMessage {
	msg := ChatMessage{Role: "assistant"}
	var text strings.Builder
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(b.Text)
		case types.BlockToolUse:
			if b.Use == nil {
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
	}
	msg.Content = text.String()
	return msg
}

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

// FormatToolArgs renders tool input back to a JSON string for assistant replay.
func FormatToolArgs(input map[string]any) (string, error) {
	if input == nil {
		return "{}", nil
	}
	b, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal tool args: %w", err)
	}
	return string(b), nil
}
