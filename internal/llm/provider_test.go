package llm

import "testing"

func TestParseThinking(t *testing.T) {
	cases := map[string]Thinking{
		"off":     ThinkingOff,
		"low":     ThinkingLow,
		"Medium":  ThinkingMedium,
		"med":     ThinkingMedium,
		"HIGH":    ThinkingHigh,
		"auto":    ThinkingAuto,
		"AUTO":    ThinkingAuto,
		"":        ThinkingOff,
		"garbage": ThinkingOff,
		"  high ": ThinkingHigh,
	}
	for in, want := range cases {
		if got := ParseThinking(in); got != want {
			t.Errorf("ParseThinking(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveThinking(t *testing.T) {
	cases := []struct {
		level Thinking
		model string
		want  Thinking
	}{
		// auto + reasoning model → medium
		{ThinkingAuto, "o3-mini", ThinkingMedium},
		{ThinkingAuto, "openai/o4-mini", ThinkingMedium},
		{ThinkingAuto, "gpt-5-codex", ThinkingMedium},
		{ThinkingAuto, "anthropic/claude-sonnet-4-6", ThinkingMedium},
		{ThinkingAuto, "claude-haiku-4-5", ThinkingMedium},
		{ThinkingAuto, "gemini-2.5-pro", ThinkingMedium},
		{ThinkingAuto, "deepseek-reasoner", ThinkingMedium},
		{ThinkingAuto, "deepseek/deepseek-v4-flash", ThinkingMedium},
		{ThinkingAuto, "qwq-32b", ThinkingMedium},
		// auto + non-reasoning model → off (so adapters omit the field)
		{ThinkingAuto, "gpt-4o-mini", ThinkingOff},
		{ThinkingAuto, "llama-3.1-8b-instant", ThinkingOff},
		{ThinkingAuto, "deepseek-chat", ThinkingOff},
		{ThinkingAuto, "claude-3-5-sonnet", ThinkingOff},
		{ThinkingAuto, "", ThinkingOff},
		// explicit levels pass through unchanged regardless of model
		{ThinkingOff, "o3-mini", ThinkingOff},
		{ThinkingHigh, "gpt-4o-mini", ThinkingHigh},
		{ThinkingLow, "", ThinkingLow},
	}
	for _, c := range cases {
		if got := ResolveThinking(c.level, c.model); got != c.want {
			t.Errorf("ResolveThinking(%q,%q) = %q, want %q", c.level, c.model, got, c.want)
		}
	}
}

func TestSupportsThinking(t *testing.T) {
	yes := []string{
		"o1", "o1-mini", "o3", "o3-mini", "o4-mini",
		"gpt-5", "gpt-5-codex", "openai/gpt-5-pro",
		"claude-opus-4-1", "claude-sonnet-4-6", "claude-haiku-4-5",
		"anthropic/claude-sonnet-4-5",
		"deepseek-reasoner", "deepseek/deepseek-v4-flash", "deepseek-r1",
		"gemini-2.5-pro", "gemini-2.5-flash",
		"qwq-32b-preview", "qwen3-thinking",
		"glm-4.6",
		"x-ai/grok-4", "grok-3-mini",
		"moonshot/kimi-k2",
	}
	for _, m := range yes {
		if !SupportsThinking(m) {
			t.Errorf("SupportsThinking(%q) = false, want true", m)
		}
	}
	no := []string{
		"", "gpt-4o", "gpt-4o-mini", "gpt-3.5-turbo",
		"claude-3-5-sonnet", "claude-3-haiku",
		"deepseek-chat", "deepseek-coder",
		"gemini-2.0-flash", "gemini-1.5-pro",
		"llama-3.1-8b-instant", "llama-3.3-70b-versatile",
		"qwen2.5-coder-7b", "mistral-large",
	}
	for _, m := range no {
		if SupportsThinking(m) {
			t.Errorf("SupportsThinking(%q) = true, want false", m)
		}
	}
}

func TestBuildAnthropicBody_ThinkingMapping(t *testing.T) {
	if got := buildAnthropicBody(Request{Thinking: ThinkingOff}); got != nil {
		t.Errorf("off should yield nil, got: %+v", got)
	}
	if got := buildAnthropicBody(Request{}); got != nil {
		t.Errorf("empty should yield nil, got: %+v", got)
	}
	got := buildAnthropicBody(Request{Thinking: ThinkingHigh})
	if got == nil {
		t.Fatal("high should yield a body")
	}
	if got.Type != "enabled" {
		t.Errorf("type: %q", got.Type)
	}
	if got.BudgetTokens != ThinkingBudget(ThinkingHigh) {
		t.Errorf("budget: %d", got.BudgetTokens)
	}
}

func TestThinkingBudget(t *testing.T) {
	if ThinkingBudget(ThinkingOff) != 0 {
		t.Error("off should be 0")
	}
	if ThinkingBudget(ThinkingHigh) <= ThinkingBudget(ThinkingMedium) {
		t.Error("high > medium expected")
	}
	if ThinkingBudget(ThinkingMedium) <= ThinkingBudget(ThinkingLow) {
		t.Error("medium > low expected")
	}
	if ThinkingBudget(ThinkingLow) <= 0 {
		t.Error("low > 0 expected")
	}
}
