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

	// an act turn dropped ask_user (plan-only); a grant re-adds it.
	act := filterToolSpecsForRole([]llm.ToolSpec{{Name: "read"}}, RoleWorker, false)
	got := applySkillToolGrants(act, reg, []string{"ask_user"})
	if !hasSpec(got, "ask_user") {
		t.Error("grant should re-add plan-only ask_user")
	}

	// a non-plan-only grant (write) must NOT be force-added on a read-only turn —
	// the read-only guarantee stays intact.
	ro := filterToolSpecsForRole([]llm.ToolSpec{{Name: "read"}}, RoleScout, true)
	got = applySkillToolGrants(ro, reg, []string{"write"})
	if hasSpec(got, "write") {
		t.Error("grant must not re-enable non-plan-only write on a read-only turn")
	}

	// nil grant and nil registry are no-ops.
	if out := applySkillToolGrants(act, nil, []string{"ask_user"}); len(out) != len(act) {
		t.Error("nil registry should be a no-op")
	}
	if out := applySkillToolGrants(act, reg, nil); len(out) != len(act) {
		t.Error("nil grant should be a no-op")
	}
}

func TestParseRole(t *testing.T) {
	cases := map[string]Role{
		"worker":     RoleWorker,
		"WORKER":     RoleWorker,
		" scout ":    RoleScout,
		"queen":      RoleQueen,
		"QUEEN":      RoleQueen,
		"":           RoleWorker,
		"junk":       RoleWorker,
		"plan":       RoleScout,  // legacy mode
		"auto":       RoleWorker, // legacy mode
		"edit":       RoleWorker, // legacy mode
		"yolo":       RoleWorker, // legacy mode (toggle handled separately)
		"mastermind": RoleQueen,  // legacy effort tier
	}
	for in, want := range cases {
		if got := ParseRole(in); got != want {
			t.Errorf("ParseRole(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRoleThinking(t *testing.T) {
	cases := map[Role]llm.Thinking{
		RoleWorker: llm.ThinkingAuto,
		RoleScout:  llm.ThinkingHigh,
		RoleQueen:  llm.ThinkingMax,
	}
	for r, want := range cases {
		if got := RoleThinking(r); got != want {
			t.Errorf("RoleThinking(%q) = %q, want %q", r, got, want)
		}
	}
}

func TestParseClassifyReadOnly(t *testing.T) {
	cases := map[string]bool{
		"plan":         true,
		"  PLAN.":      true,
		"\"plan\"":     true,
		"plan mode":    true,
		"edit":         false,
		"unknown":      false,
		"":             false,
		"let me think": false,
	}
	for in, want := range cases {
		if got := parseClassifyReadOnly(in); got != want {
			t.Errorf("parseClassifyReadOnly(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFilterToolSpecsForRole(t *testing.T) {
	specs := []llm.ToolSpec{
		{Name: "read"},
		{Name: "search"},
		{Name: "bash"},
		{Name: "edit"},
		{Name: "write"},
		{Name: "knowledge_search"},
		{Name: "web_search"},
	}
	// worker act turn passes everything through (no plan-only tools present)
	if got := filterToolSpecsForRole(specs, RoleWorker, false); len(got) != len(specs) {
		t.Errorf("worker act should pass through %d specs, got %d", len(specs), len(got))
	}
	// a read-only worker turn drops mutators (and web — worker read-only is not scout)
	got := filterToolSpecsForRole(specs, RoleWorker, true)
	for _, s := range got {
		if !readOnlyTools[s.Name] {
			t.Errorf("worker read-only leaked %q", s.Name)
		}
	}
	if len(got) != 3 { // read, search, knowledge_search
		t.Errorf("worker read-only set size = %d, want 3", len(got))
	}
	// scout keeps the read-only whitelist PLUS web tools
	scout := filterToolSpecsForRole(specs, RoleScout, true)
	if !hasSpec(scout, "web_search") {
		t.Error("scout should keep web_search")
	}
	for _, s := range scout {
		if !readOnlyTools[s.Name] && !scoutExtraTools[s.Name] {
			t.Errorf("scout leaked mutator %q", s.Name)
		}
	}
	if len(scout) != 4 { // read, search, knowledge_search, web_search
		t.Errorf("scout set size = %d, want 4", len(scout))
	}
}

func TestFilterToolSpecsForRole_AskUserPlanOnly(t *testing.T) {
	specs := []llm.ToolSpec{{Name: "read"}, {Name: "ask_user"}, {Name: "write"}}

	// read-only turn keeps ask_user (it's read-only-safe)
	ro := filterToolSpecsForRole(specs, RoleScout, true)
	if !hasSpec(ro, "ask_user") {
		t.Error("read-only turn should keep ask_user")
	}
	// act turn drops ask_user (plan-only) but keeps everything else
	act := filterToolSpecsForRole(specs, RoleWorker, false)
	if hasSpec(act, "ask_user") {
		t.Error("act turn should drop plan-only ask_user")
	}
	if !hasSpec(act, "read") || !hasSpec(act, "write") {
		t.Error("act turn should keep non-plan-only tools")
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

func TestRolePromptPrefix(t *testing.T) {
	if rolePromptPrefix(RoleWorker, false) != "" {
		t.Error("act turn must have empty prefix")
	}
	if rolePromptPrefix(RoleWorker, true) == "" {
		t.Error("read-only worker turn prefix must be non-empty")
	}
	scout := rolePromptPrefix(RoleScout, true)
	if scout == "" {
		t.Error("scout prefix must be non-empty")
	}
	// scout gets the extra web nudge that a plain read-only worker turn doesn't.
	if scout == rolePromptPrefix(RoleWorker, true) {
		t.Error("scout prefix should add the web-tools nudge")
	}
}
