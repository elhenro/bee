package llm

import "testing"

func TestSupportsVision(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-7", true},
		{"anthropic/claude-haiku-4-5", true},
		{"gpt-4o", true},
		{"gpt-5.4", true},
		{"openai/o3", true},
		{"gemini-3-pro-preview", true},
		{"qwen3-vl-it", true},
		{"Qwen2.5-VL-7B", true},
		{"llava:13b", true},
		{"llama-3.2-11b-vision", true},
		// non-vision / text-only
		{"deepseek-v4-flash", false},
		{"deepseek-chat", false},
		{"qwen3-coder", false},
		{"llama-3.3-70b-versatile", false},
		{"", false},
	}
	for _, c := range cases {
		if got := SupportsVision(c.id); got != c.want {
			t.Errorf("SupportsVision(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}
