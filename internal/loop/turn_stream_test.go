package loop

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestIsProviderRejectErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		// positive: each marker
		{"openrouter minimax", errors.New("provider openrouter status 400: invalid function arguments json string, tool_call_id: call_function_9cgx2d7tk613_1 (2013)"), true},
		{"invalid tool_call", errors.New("provider openai status 400: invalid tool_call: missing function"), true},
		{"anthropic invalid tool call", errors.New("provider anthropic status 400: messages.0.content.0: invalid tool call"), true},
		{"tool_call_id phrasing", errors.New("provider openrouter status 400: bad, tool_call_id: abc"), true},
		{"anthropic tool_use input", errors.New("provider anthropic status 400: messages.1.content.1.tool_use.input: Input should be a valid dictionary"), true},
		{"strict mode field", errors.New("provider openai status 400: tools.0.function.arguments must be valid JSON"), true},

		// negative: 400 but unrelated; the bare envelope names wrap these too
		{"envelope alone", errors.New("provider anthropic status 400: invalid_request_error: tools[0].input_schema invalid"), false},
		{"invalid parameters envelope", errors.New("provider openai status 400: invalid parameters: function name"), false},
		{"invalid_argument envelope", errors.New("provider gemini status 400: invalid_argument: bad tool"), false},
		{"prompt too long", errors.New(`provider anthropic status 400: {"type":"invalid_request_error","message":"prompt is too long: 215000 tokens > 200000 maximum"}`), false},
		{"credit balance", errors.New(`provider anthropic status 400: {"type":"invalid_request_error","message":"Your credit balance is too low to access the Anthropic API"}`), false},
		{"400 auth", errors.New("provider openrouter status 400: invalid api key"), false},
		{"401", errors.New("provider openai status 401: unauthorized"), false},
		{"429", errors.New("provider openai status 429: rate limit exceeded"), false},
		{"500", errors.New("provider openai status 500: internal"), false},
		{"transport", errors.New("dial tcp: connection refused"), false},
		{"nil", nil, false},
		{"empty", errors.New(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isProviderRejectErr(tc.err)
			if got != tc.want {
				t.Errorf("isProviderRejectErr(%q) = %v, want %v", errString(tc.err), got, tc.want)
			}
		})
	}
}

func errString(e error) string {
	if e == nil {
		return "<nil>"
	}
	return e.Error()
}

// TestProviderRejectError_Is chains: errors.Is must match the sentinel so
// resume.go's classification works without caring which transport produced it.
func TestProviderRejectError_Is(t *testing.T) {
	raw := "provider openrouter status 400: invalid function arguments, tool_call_id: x"
	wrapped := &ProviderRejectError{Raw: raw}
	if !errors.Is(wrapped, ErrProviderReject) {
		t.Fatal("errors.Is(wrapped, ErrProviderReject) = false, want true")
	}
	if wrapped.Error() == "" {
		t.Fatal("Error() empty")
	}
	if !strings.Contains(wrapped.Error(), raw) {
		t.Errorf("Error() = %q, want substring %q", wrapped.Error(), raw)
	}
}

func TestIsContextOverflowErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"anthropic prompt too long", errors.New(`provider anthropic status 400: {"type":"invalid_request_error","message":"prompt is too long: 215000 tokens > 200000 maximum"}`), true},
		{"openai code", errors.New(`provider openai status 400: {"error":{"code":"context_length_exceeded"}}`), true},
		{"openai prose", errors.New("provider openrouter status 400: This model's maximum context length is 128000 tokens"), true},
		{"responses prose", errors.New("provider chatgpt status 400: Your input exceeds the context window of this model"), true},
		{"too many tokens", errors.New("provider groq status 413: Too many tokens in request"), true},
		{"request too large", errors.New(`provider anthropic status 413: {"type":"request_too_large"}`), true},
		{"wrapped", fmt.Errorf("provider stream: %w", errors.New("provider anthropic status 400: prompt is too long")), true},

		{"credit balance", errors.New("provider anthropic status 400: Your credit balance is too low"), false},
		{"tool args", errors.New("provider openai status 400: invalid function arguments"), false},
		{"500 with marker", errors.New("provider openai status 500: maximum context length"), false},
		{"transport", errors.New("dial tcp: connection refused"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isContextOverflowErr(tc.err); got != tc.want {
				t.Errorf("isContextOverflowErr(%q) = %v, want %v", errString(tc.err), got, tc.want)
			}
		})
	}
}
