package llm

import "testing"

func TestParseThinking(t *testing.T) {
	cases := map[string]Thinking{
		"off":     ThinkingOff,
		"low":     ThinkingLow,
		"Medium":  ThinkingMedium,
		"med":     ThinkingMedium,
		"HIGH":    ThinkingHigh,
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
