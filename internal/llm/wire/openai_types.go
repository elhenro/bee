package wire

// ChatRequest is the OpenAI chat completions body.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ChatTool    `json:"tools,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
	// StreamOptions opts in to per-stream extras. Required (include_usage=true)
	// for OpenAI/OpenRouter/DeepSeek to emit token counts on the final SSE
	// chunk; otherwise the stream finishes without a Usage event.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`
	// ReasoningEffort is OpenAI's o-series extended-reasoning knob. Values:
	// "low" | "medium" | "high". Omitted when empty.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// ChatTemplateKwargs is an MLX/vllm extension to OpenAI's chat completions
	// body — kwargs forwarded into the model's chat template at apply time.
	// Used to flip Qwen3 / Hermes template switches like `enable_thinking=false`
	// or `tools=true` that flip the model out of prose-summary mode into the
	// `<tool_call>` envelope shape bee expects. Omitted when empty.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	// Usage opts in to provider-reported actual spend on the final usage block.
	// Only routed aggregators understand it; strict OpenAI-style endpoints
	// reject unknown body fields, so it is gated (set only when the provider
	// advertises the capability) rather than always emitted.
	Usage *UsageRequest `json:"usage,omitempty"`
	// KeepAlive is Ollama's extension controlling how long the model stays
	// resident after a request (e.g. "15m"). Omitted when empty; only set for
	// local providers that understand it, so strict endpoints never see it.
	KeepAlive string `json:"keep_alive,omitempty"`
	// NumCtx is Ollama's extension setting the runtime context window. Without
	// it ollama runs at its 4096 default and silently truncates the prompt head
	// even when the model can do far more. Omitted when zero; only set for local
	// ollama-shaped servers, so strict endpoints never see it.
	NumCtx int `json:"num_ctx,omitempty"`
	// ResponseFormat requests structured output. Grammar-capable servers
	// (ollama ≥0.5, llama.cpp, MLX servers with xgrammar) compile the schema
	// to a sampling constraint, so the completion is valid by construction.
	// Omitted when nil.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	// Provider is the routed-aggregator upstream preference (e.g. sort by price).
	// Omitted when nil; only routers that understand it get it, so strict
	// endpoints never see the field.
	Provider *ProviderRouting `json:"provider,omitempty"`
}

// ProviderRouting steers which upstream a routed aggregator picks. Sort is
// "price" | "throughput" | "latency".
type ProviderRouting struct {
	Sort string `json:"sort,omitempty"`
}

// ResponseFormat is OpenAI's structured-output envelope. Type is
// "json_schema" (with JSONSchema set) or "json_object" (schema-free).
type ResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *JSONSchemaSpec `json:"json_schema,omitempty"`
}

// JSONSchemaSpec names a schema for response_format type "json_schema".
type JSONSchemaSpec struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict,omitempty"`
	Schema map[string]any `json:"schema"`
}

// StreamOptions matches OpenAI's stream_options envelope. Only include_usage
// is used today.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// UsageRequest asks the service to include real cost in the usage block.
type UsageRequest struct {
	Include bool `json:"include"`
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

// ChatContentPart is one typed part of a message content array. Used to attach
// a prompt-cache breakpoint to a text part (OpenRouter / Anthropic-via-router
// honor cache_control on content parts, mirroring Anthropic's native API).
type ChatContentPart struct {
	Type         string              `json:"type"`
	Text         string              `json:"text,omitempty"`
	CacheControl *OpenAICacheControl `json:"cache_control,omitempty"`
}

// OpenAICacheControl marks a content part as an ephemeral prompt-cache
// breakpoint (same shape as Anthropic's cache_control).
type OpenAICacheControl struct {
	Type string `json:"type"`
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
