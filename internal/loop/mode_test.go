package loop

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// fakeTool is a minimal tools.Tool for registry-backed tests.
type fakeTool struct{ name string }

func (f fakeTool) Spec() llm.ToolSpec { return llm.ToolSpec{Name: f.name} }
func (f fakeTool) Run(context.Context, map[string]any) (tools.Result, error) {
	return tools.Result{}, nil
}

func TestApplySkillToolGrants(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(fakeTool{"ask_user"})
	_ = reg.Register(fakeTool{"write"})

	// edit mode dropped ask_user (plan-only); a grant re-adds it.
	edit := filterToolSpecsForMode([]llm.ToolSpec{{Name: "read"}}, ModeEdit)
	got := applySkillToolGrants(edit, reg, []string{"ask_user"})
	if !hasSpec(got, "ask_user") {
		t.Error("grant should re-add plan-only ask_user")
	}

	// a non-plan-only grant (write) must NOT be force-added in plan mode —
	// plan mode's read-only guarantee stays intact.
	plan := filterToolSpecsForMode([]llm.ToolSpec{{Name: "read"}}, ModePlan)
	got = applySkillToolGrants(plan, reg, []string{"write"})
	if hasSpec(got, "write") {
		t.Error("grant must not re-enable non-plan-only write in plan mode")
	}

	// nil grant and nil registry are no-ops.
	if out := applySkillToolGrants(edit, nil, []string{"ask_user"}); len(out) != len(edit) {
		t.Error("nil registry should be a no-op")
	}
	if out := applySkillToolGrants(edit, reg, nil); len(out) != len(edit) {
		t.Error("nil grant should be a no-op")
	}
}

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
		{Name: "search"},
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
	if len(got) != 3 { // read, search, knowledge_search
		t.Errorf("ModePlan filtered set size = %d, want 3", len(got))
	}
}

func TestFilterToolSpecsForMode_AskUserPlanOnly(t *testing.T) {
	specs := []llm.ToolSpec{{Name: "read"}, {Name: "ask_user"}, {Name: "write"}}

	// plan mode keeps ask_user (it's plan-safe)
	plan := filterToolSpecsForMode(specs, ModePlan)
	if !hasSpec(plan, "ask_user") {
		t.Error("ModePlan should keep ask_user")
	}
	// edit mode drops ask_user (plan-only) but keeps everything else
	edit := filterToolSpecsForMode(specs, ModeEdit)
	if hasSpec(edit, "ask_user") {
		t.Error("ModeEdit should drop plan-only ask_user")
	}
	if !hasSpec(edit, "read") || !hasSpec(edit, "write") {
		t.Error("ModeEdit should keep non-plan-only tools")
	}
}

func hasSpec(specs []llm.ToolSpec, name string) bool {
	for _, s := range specs {
		if s.Name == name {
			return true
		}
	}
	return false
}

func TestModePromptPrefix(t *testing.T) {
	if modePromptPrefix(ModeEdit) != "" {
		t.Error("ModeEdit must have empty prefix")
	}
	if modePromptPrefix(ModePlan) == "" {
		t.Error("ModePlan prefix must be non-empty")
	}
}
