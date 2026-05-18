package llm

import "testing"

func TestIsQwen3HybridThinking(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"Qwen3.6-35B-A3B-4bit", true},
		{"qwen3-coder-30b", true},
		{"qwen3-235b-a22b", true},
		{"qwen-3-coder-7b", true},
		{"mlx-community/Qwen3-Coder-30B-A3B", true},
		// explicit thinking variants — toggle is redundant.
		{"qwq-32b-preview", false},
		{"qwen3-thinking-30b", false},
		{"qwen3-reasoner", false},
		// non-qwen3.
		{"qwen2.5-coder-7b", false},
		{"llama-3.1-8b", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsQwen3HybridThinking(c.id); got != c.want {
			t.Errorf("IsQwen3HybridThinking(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestQwen3ThinkingHint(t *testing.T) {
	cases := []struct {
		level Thinking
		want  string
	}{
		{ThinkingOff, "/no_think"},
		{ThinkingLow, "/no_think"},
		{ThinkingMedium, "/think"},
		{ThinkingHigh, "/think"},
		{ThinkingMax, "/think"},
		{ThinkingAuto, ""},
	}
	for _, c := range cases {
		if got := Qwen3ThinkingHint(c.level); got != c.want {
			t.Errorf("Qwen3ThinkingHint(%v) = %q, want %q", c.level, got, c.want)
		}
	}
}
