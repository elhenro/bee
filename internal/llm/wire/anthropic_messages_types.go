package wire

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
	Type         string             `json:"type"`
	Text         string             `json:"text"`
	CacheControl *AnthropicCacheCtl `json:"cache_control,omitempty"`
}

// AnthropicCacheCtl marks a block as a prompt-cache breakpoint.
type AnthropicCacheCtl struct {
	Type string `json:"type"`
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
// tool_use | tool_result | thinking.
type AnthropicContentPart struct {
	Type string `json:"type"`
	// text
	Text string `json:"text,omitempty"`
	// image
	Source *AnthropicImageSource `json:"source,omitempty"`
	// tool_use
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// any, not a map: omitempty drops an empty map, and tool_use requires input
	Input any `json:"input,omitempty"`
	// thinking
	Thinking  *string `json:"thinking,omitempty"`
	Signature string  `json:"signature,omitempty"`
	// tool_result
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	IsError   bool                   `json:"is_error,omitempty"`
	Content   []AnthropicContentPart `json:"content,omitempty"`
	// CacheControl marks this block as a prompt-cache breakpoint. Placed on the
	// last block of recent turns so the conversation prefix caches across turns.
	CacheControl *AnthropicCacheCtl `json:"cache_control,omitempty"`
}

// AnthropicImageSource is the base64-payload image envelope.
type AnthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

// AnthropicTool advertises a function tool.
type AnthropicTool struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	InputSchema  map[string]any     `json:"input_schema"`
	CacheControl *AnthropicCacheCtl `json:"cache_control,omitempty"`
}
