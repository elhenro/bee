// responses.go covers the Responses API schema used by the chatgpt.com
// subscription backend.
//
// The shape is different from /chat/completions: messages are a flat "input"
// list of items (each role+content array of typed parts, plus function_call
// / function_call_output items), tools are bare {type,name,description,parameters}
// (no nested function envelope), and streaming events are typed
// (response.output_text.delta etc) rather than incremental message deltas.
package wire

import (
	"encoding/base64"
	"encoding/json"

	"github.com/elhenro/bee/internal/types"
)

// ResponsesRequest is the request body for POST /responses.
type ResponsesRequest struct {
	Model        string             `json:"model"`
	Instructions string             `json:"instructions,omitempty"`
	Input        []ResponsesItem    `json:"input"`
	Tools        []ResponsesTool    `json:"tools,omitempty"`
	Stream       bool               `json:"stream,omitempty"`
	MaxOutput    int                `json:"max_output_tokens,omitempty"`
	Temperature  *float64           `json:"temperature,omitempty"`
	Reasoning    *ResponsesReason   `json:"reasoning,omitempty"`
	// Store=false keeps the call ephemeral on the server side. The
	// backend appears to default to false but we set it explicitly.
	Store *bool `json:"store,omitempty"`
}

// ResponsesReason carries the reasoning-effort hint for reasoning models.
type ResponsesReason struct {
	Effort string `json:"effort,omitempty"`
}

// ResponsesItem is one entry in the flat input list. Type is one of:
// "message" (role+content), "function_call", "function_call_output". For
// "message" items, Content is an array of typed parts. For function items,
// CallID/Name/Arguments/Output carry the payload directly.
type ResponsesItem struct {
	Type      string             `json:"type,omitempty"`
	Role      string             `json:"role,omitempty"`
	Content   []ResponsesContent `json:"content,omitempty"`
	CallID    string             `json:"call_id,omitempty"`
	Name      string             `json:"name,omitempty"`
	Arguments string             `json:"arguments,omitempty"`
	Output    string             `json:"output,omitempty"`
	Status    string             `json:"status,omitempty"`
}

// ResponsesContent is one typed content part. Types: "input_text",
// "output_text", "input_image".
type ResponsesContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// ResponsesTool advertises a function tool. Flat shape (no nested function
// envelope like chat/completions uses).
type ResponsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// BuildResponsesRequest converts internal Request shape to the Responses API
// body. System prompt maps to instructions (not a message). Tool calls and
// tool results become flat function_call / function_call_output items.
func BuildResponsesRequest(model, system string, messages []types.Message, tools []ToolAdvert, maxTokens int, temperature float64, stream bool, reasoningEffort string) ResponsesRequest {
	req := ResponsesRequest{
		Model:        model,
		Instructions: system,
		Stream:       stream,
	}
	if maxTokens > 0 {
		req.MaxOutput = maxTokens
	}
	if temperature != 0 {
		t := temperature
		req.Temperature = &t
	}
	if reasoningEffort != "" {
		req.Reasoning = &ResponsesReason{Effort: reasoningEffort}
	}
	storeFalse := false
	req.Store = &storeFalse

	for _, m := range messages {
		req.Input = append(req.Input, translateResponsesMessage(m)...)
	}
	for _, t := range tools {
		req.Tools = append(req.Tools, ResponsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Schema,
		})
	}
	return req
}

// translateResponsesMessage emits 1..N Responses items for one internal
// Message. Tool-use blocks become function_call items (separate from the
// assistant message). Tool-result blocks become function_call_output items.
func translateResponsesMessage(m types.Message) []ResponsesItem {
	switch m.Role {
	case types.RoleSystem:
		// Responses API uses instructions, not a system message. If the loop
		// ever appends a system mid-stream, route it as a user message so it
		// still lands.
		text := collectText(m.Content)
		if text == "" {
			return nil
		}
		return []ResponsesItem{{
			Type:    "message",
			Role:    "system",
			Content: []ResponsesContent{{Type: "input_text", Text: text}},
		}}

	case types.RoleUser:
		return splitResponsesUser(m.Content)

	case types.RoleAssistant:
		return splitResponsesAssistant(m.Content)

	case types.RoleTool:
		return splitResponsesUser(m.Content)
	}
	return nil
}

func splitResponsesUser(blocks []types.ContentBlock) []ResponsesItem {
	var items []ResponsesItem
	var parts []ResponsesContent
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			parts = append(parts, ResponsesContent{Type: "input_text", Text: b.Text})
		case types.BlockImage:
			// Responses API images: input_image with a data URL or remote URL.
			// We keep it simple — emit a text marker if data isn't a URL.
			if len(b.Data) > 0 {
				mt := b.MediaType
				if mt == "" {
					mt = "image/png"
				}
				parts = append(parts, ResponsesContent{
					Type:     "input_image",
					ImageURL: "data:" + mt + ";base64," + encodeBase64(b.Data),
				})
			}
		case types.BlockToolResult:
			if b.Result == nil {
				continue
			}
			content := b.Result.Content
			if b.Result.IsError {
				content = "ERROR: " + content
			}
			// responses api requires non-empty output on function_call_output;
			// empty string gets dropped by omitempty and returns 400
			if content == "" {
				content = "(no output)"
			}
			items = append(items, ResponsesItem{
				Type:   "function_call_output",
				CallID: b.Result.UseID,
				Output: content,
			})
		}
	}
	if len(parts) > 0 {
		items = append([]ResponsesItem{{
			Type:    "message",
			Role:    "user",
			Content: parts,
		}}, items...)
	}
	return items
}

func splitResponsesAssistant(blocks []types.ContentBlock) []ResponsesItem {
	var items []ResponsesItem
	var parts []ResponsesContent
	for _, b := range blocks {
		switch b.Type {
		case types.BlockText:
			parts = append(parts, ResponsesContent{Type: "output_text", Text: b.Text})
		case types.BlockToolUse:
			if b.Use == nil {
				continue
			}
			args, _ := json.Marshal(b.Use.Input)
			items = append(items, ResponsesItem{
				Type:      "function_call",
				CallID:    b.Use.ID,
				Name:      b.Use.Name,
				Arguments: string(args),
			})
		}
	}
	if len(parts) > 0 {
		items = append([]ResponsesItem{{
			Type:    "message",
			Role:    "assistant",
			Content: parts,
		}}, items...)
	}
	return items
}

func encodeBase64(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
