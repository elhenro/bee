package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/llm"
)

func TestParseMode(t *testing.T) {
	cases := map[string]Mode{
		"plan":   ModePlan,
		"PLAN":   ModePlan,
		" auto ": ModeAuto,
		"edit":   ModeEdit,
		"":       ModeEdit,
		"junk":   ModeEdit,
	}
	for in, want := range cases {
		if got := ParseMode(in); got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseClassifyOutput(t *testing.T) {
	cases := map[string]Mode{
		"plan":         ModePlan,
		"  PLAN.":      ModePlan,
		"\"plan\"":     ModePlan,
		"plan mode":    ModePlan,
		"edit":         ModeEdit,
		"unknown":      ModeEdit,
		"":             ModeEdit,
		"let me think": ModeEdit,
	}
	for in, want := range cases {
		if got := parseClassifyOutput(in); got != want {
			t.Errorf("parseClassifyOutput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFilterToolSpecsForMode(t *testing.T) {
	specs := []llm.ToolSpec{
		{Name: "read"},
		{Name: "grep"},
		{Name: "bash"},
		{Name: "edit"},
		{Name: "write"},
		{Name: "knowledge_search"},
	}
	// edit passes through
	if got := filterToolSpecsForMode(specs, ModeEdit); len(got) != len(specs) {
		t.Errorf("ModeEdit should pass through %d specs, got %d", len(specs), len(got))
	}
	// plan drops mutators
	got := filterToolSpecsForMode(specs, ModePlan)
	for _, s := range got {
		if !planSafeTools[s.Name] {
			t.Errorf("ModePlan leaked mutator %q", s.Name)
		}
	}
	if len(got) != 3 { // read, grep, knowledge_search
		t.Errorf("ModePlan filtered set size = %d, want 3", len(got))
	}
}

func TestModePromptPrefix(t *testing.T) {
	if modePromptPrefix(ModeEdit) != "" {
		t.Error("ModeEdit must have empty prefix")
	}
	if modePromptPrefix(ModePlan) == "" {
		t.Error("ModePlan prefix must be non-empty")
	}
}
