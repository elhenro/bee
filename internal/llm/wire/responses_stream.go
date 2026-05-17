package wire

import (
	"encoding/json"
	"fmt"
)

// ResponsesEvent is one parsed SSE event from a /responses stream. Type names
// follow the Responses API spec: response.created, response.output_text.delta,
// response.function_call_arguments.delta, response.output_item.added,
// response.output_item.done, response.completed, response.failed.
type ResponsesEvent struct {
	Type     string                 `json:"type"`
	Response *ResponsesEventBody    `json:"response,omitempty"`
	Item     *ResponsesOutputItem   `json:"item,omitempty"`
	Delta    string                 `json:"delta,omitempty"`
	OutputIndex *int                `json:"output_index,omitempty"`
	ItemID   string                 `json:"item_id,omitempty"`
	CallID   string                 `json:"call_id,omitempty"`
	// Arguments-done event includes the full arguments string.
	Arguments string `json:"arguments,omitempty"`
}

// ResponsesEventBody carries the response-level payload on created/completed/failed.
type ResponsesEventBody struct {
	ID     string                 `json:"id"`
	Status string                 `json:"status"`
	Usage  *ResponsesUsage        `json:"usage"`
	Output []ResponsesOutputItem  `json:"output"`
	Error  *ResponsesErrorPayload `json:"error"`
}

// ResponsesOutputItem is one entry in the final output list. Type is
// "message" or "function_call". For function_call items the call id, name,
// and arguments string are flat fields.
type ResponsesOutputItem struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	Status    string             `json:"status"`
	Role      string             `json:"role,omitempty"`
	Content   []ResponsesContent `json:"content,omitempty"`
	CallID    string             `json:"call_id,omitempty"`
	Name      string             `json:"name,omitempty"`
	Arguments string             `json:"arguments,omitempty"`
}

// ResponsesUsage matches the usage block on the final event.
type ResponsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ResponsesErrorPayload is the error envelope on failed responses.
type ResponsesErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ParseResponsesEvent decodes one SSE `data:` payload.
func ParseResponsesEvent(data []byte) (*ResponsesEvent, error) {
	trimmed := trimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if string(trimmed) == "[DONE]" {
		// Responses API doesn't use [DONE], but be defensive.
		return &ResponsesEvent{Type: "done"}, nil
	}
	var ev ResponsesEvent
	if err := json.Unmarshal(trimmed, &ev); err != nil {
		return nil, fmt.Errorf("decode responses event: %w", err)
	}
	return &ev, nil
}
