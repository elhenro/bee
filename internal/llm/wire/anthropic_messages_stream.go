package wire

import (
	"encoding/json"
)

// AnthropicStreamEvent is the parsed SSE event envelope. Field set depends
// on Type: text_delta carries Delta; input_json_delta carries PartialJSON;
// message_delta carries StopReason + Usage; content_block_start announces
// a new block (text or tool_use); message_start carries initial usage.
type AnthropicStreamEvent struct {
	Type string `json:"type"`

	Index int `json:"index,omitempty"`

	Delta *AnthropicDeltaPayload `json:"delta,omitempty"`

	ContentBlock *AnthropicContentBlockStart `json:"content_block,omitempty"`

	Message *AnthropicStreamMessage `json:"message,omitempty"`

	Usage *AnthropicUsage `json:"usage,omitempty"`
}

// AnthropicDeltaPayload covers both content_block_delta (text_delta /
// input_json_delta / thinking_delta / signature_delta) and message_delta
// (stop_reason).
type AnthropicDeltaPayload struct {
	Type        string `json:"type,omitempty"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

// AnthropicContentBlockStart is the body of `content_block_start` events.
// For tool_use blocks we get name + id up front (input is empty, filled by
// input_json_delta).
type AnthropicContentBlockStart struct {
	Type  string         `json:"type"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

// AnthropicStreamMessage is the body of `message_start` (carries initial
// usage with cache stats) and `message_stop` (sometimes empty).
type AnthropicStreamMessage struct {
	ID           string          `json:"id,omitempty"`
	Model        string          `json:"model,omitempty"`
	Role         string          `json:"role,omitempty"`
	StopReason   string          `json:"stop_reason,omitempty"`
	StopSequence string          `json:"stop_sequence,omitempty"`
	Usage        *AnthropicUsage `json:"usage,omitempty"`
}

// AnthropicUsage is the token-accounting block.
type AnthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ParseAnthropicEvent parses one SSE `data:` line payload. Returns nil for
// events with no useful fields (ping / unknown types) so callers can skip.
func ParseAnthropicEvent(data []byte) (*AnthropicStreamEvent, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var ev AnthropicStreamEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, err
	}
	if ev.Type == "" {
		return nil, nil
	}
	return &ev, nil
}
