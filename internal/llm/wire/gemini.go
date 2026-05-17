// Package wire — gemini.go covers Google's generativelanguage.googleapis.com
// v1beta schema for streamGenerateContent. The shape is meaningfully different
// from OpenAI's: messages are `contents[]`, each with typed `parts[]`, and
// system text goes in a top-level systemInstruction field.
package wire

// GeminiContent is one entry in the contents[] array. Role is "user" or
// "model"; Gemini does not have a separate "tool" role — tool results are
// user-role messages carrying functionResponse parts.
type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart is one element of a content's parts[]. Exactly one of Text /
// InlineData / FunctionCall / FunctionResponse is set per part.
type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	InlineData       *GeminiInlineData       `json:"inline_data,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
}

// GeminiInlineData carries a base64-encoded blob (image, audio, etc.).
type GeminiInlineData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

// GeminiFunctionCall is the model's request to invoke a function.
type GeminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// GeminiFunctionResponse is the caller's reply carrying the function output.
type GeminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// GeminiTool wraps a set of function declarations exposed to the model.
type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDecl `json:"function_declarations"`
}

// GeminiFunctionDecl advertises one tool with a JSON-schema-shaped Parameters.
type GeminiFunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// GeminiRequest is the POST body for :streamGenerateContent.
type GeminiRequest struct {
	Contents          []GeminiContent `json:"contents"`
	SystemInstruction *GeminiContent  `json:"systemInstruction,omitempty"`
	Tools             []GeminiTool    `json:"tools,omitempty"`
	GenerationConfig  map[string]any  `json:"generationConfig,omitempty"`
}

// GeminiStreamChunk is one SSE-delimited JSON object in the stream response.
type GeminiStreamChunk struct {
	Candidates    []GeminiCandidate `json:"candidates"`
	UsageMetadata *GeminiUsage      `json:"usageMetadata,omitempty"`
}

// GeminiCandidate is one alternative response within a chunk.
type GeminiCandidate struct {
	Content      GeminiContent `json:"content"`
	FinishReason string        `json:"finishReason,omitempty"`
}

// GeminiUsage captures Gemini's token accounting.
type GeminiUsage struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
}
