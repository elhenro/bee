package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

func newPlanFixture() PlanModeModel {
	m := NewPlanModeModel(DefaultStyles())
	m.SetWidth(80)
	m.Show()
	return m
}

func TestPlanMode_ShowOffersBuildOptions(t *testing.T) {
	m := newPlanFixture()
	if !m.Active {
		t.Fatal("Show should activate the picker")
	}
	// worker, fresh-worker, worker+yolo, keep-scouting = 4 options
	if len(m.options) != 4 {
		t.Fatalf("should offer 4 options, got %d", len(m.options))
	}
	if m.options[0].role != "worker" || m.options[0].fresh || m.options[0].yolo {
		t.Errorf("first option should build as plain worker, got %+v", m.options[0])
	}
	if !m.options[1].fresh {
		t.Errorf("second option should be the fresh-session build, got %+v", m.options[1])
	}
	if !m.options[2].yolo {
		t.Errorf("third option should arm yolo, got %+v", m.options[2])
	}
	last := m.options[len(m.options)-1]
	if last.role != "" {
		t.Errorf("last option should be keep-scouting, got %+v", last)
	}
	// focus defaults to keep-scouting so a reflexive enter is non-destructive.
	if m.focus != len(m.options)-1 {
		t.Errorf("focus should default to keep-scouting (idx %d), got %d", len(m.options)-1, m.focus)
	}
}

func TestPlanMode_EnterDefaultsToKeepPlanning(t *testing.T) {
	m := newPlanFixture()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m2.Active {
		t.Fatal("picker should deactivate after pick")
	}
	// focus defaults to keep-scouting, so a reflexive enter must not build.
	msg, ok := cmd().(PlanProceedMsg)
	if !ok || msg.Role != "" {
		t.Fatalf("got %+v, want keep-scouting (empty role)", msg)
	}
}

func TestPlanMode_NumberKeyOneBuildsWorker(t *testing.T) {
	m := newPlanFixture()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	msg := cmd().(PlanProceedMsg)
	if msg.Role != "worker" || msg.Fresh || msg.Yolo {
		t.Fatalf("key 1 should build as plain worker, got %+v", msg)
	}
}

func TestPlanMode_NumberKeyPicksFresh(t *testing.T) {
	m := newPlanFixture()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	msg := cmd().(PlanProceedMsg)
	if !msg.Fresh || msg.Role != "worker" {
		t.Fatalf("key 2 should pick fresh-session build, got %+v", msg)
	}
}

func TestPlanMode_EscDismissesNoChoice(t *testing.T) {
	m := newPlanFixture()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m2.Active {
		t.Fatal("esc should deactivate the picker")
	}
	if cmd != nil {
		t.Fatal("esc should not publish a choice")
	}
}

func TestPlanMode_ViewListsOptions(t *testing.T) {
	out := stripANSI(newPlanFixture().View())
	for _, want := range []string{"SCOUT READY", "worker", "fresh session", "yolo", "Keep scouting"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q:\n%s", want, out)
		}
	}
}

func TestLastAssistantText_PicksLastAssistant(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "build X"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "  step 1\nstep 2  "}}},
	}
	if got := lastAssistantText(msgs); got != "step 1\nstep 2" {
		t.Fatalf("got %q, want trimmed plan text", got)
	}
	if got := lastAssistantText(nil); got != "" {
		t.Fatalf("empty input should yield empty, got %q", got)
	}
}

func TestFreshContinuePrompt(t *testing.T) {
	if got := freshContinuePrompt("  "); got != continuePrompt {
		t.Fatalf("blank plan should fall back to continuePrompt, got %q", got)
	}
	got := freshContinuePrompt("do A then B")
	if !strings.Contains(got, "do A then B") || !strings.Contains(got, "Implement") {
		t.Fatalf("fresh prompt should carry the plan, got %q", got)
	}
}

func TestOnPlanProceed_KeepPlanningNoSwitch(t *testing.T) {
	m := newTestModel(t)
	m.role = "scout"
	m.pendingPlan = "the plan"
	nm, cmd := m.onPlanProceed(PlanProceedMsg{Role: ""})
	got := nm.(Model)
	if got.role != "scout" {
		t.Errorf("keep-scouting must not switch role, got %q", got.role)
	}
	if got.pendingPlan != "" {
		t.Errorf("pendingPlan should be cleared, got %q", got.pendingPlan)
	}
	if cmd != nil {
		t.Error("keep-planning should not submit a turn")
	}
}

func TestOnPlanProceed_BuildSwitchesMode(t *testing.T) {
	m := newTestModel(t)
	m.role = "scout"
	m.pendingPlan = "the plan"
	nm, cmd := m.onPlanProceed(PlanProceedMsg{Role: "worker"})
	got := nm.(Model)
	if got.role != "worker" {
		t.Errorf("build should switch to worker, got %q", got.role)
	}
	if cmd == nil {
		t.Error("build should auto-submit a continuation turn")
	}
}

func TestOnTurnDone_ShowsPickerAfterPlanTurn(t *testing.T) {
	m := newTestModel(t)
	m.role = "scout"
	m.state = StateStreaming
	res := loop.RunResult{Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "plan it"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "1. do thing"}}},
	}}
	nm, _ := m.onTurnDone(turnDoneMsg{result: res})
	got := nm.(Model)
	if !got.planmode.Active {
		t.Fatal("a clean plan-mode turn should open the post-plan picker")
	}
	if got.pendingPlan != "1. do thing" {
		t.Fatalf("pendingPlan should capture the plan, got %q", got.pendingPlan)
	}
}

func TestOnTurnDone_NoPickerWhenPlanEmpty(t *testing.T) {
	m := newTestModel(t)
	m.role = "scout"
	m.state = StateStreaming
	// assistant turn with no text block (e.g. tool/thinking only) — nothing to
	// act on or carry, so the picker must stay closed.
	res := loop.RunResult{Messages: []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockThinking, Text: "hmm"}}},
	}}
	nm, _ := m.onTurnDone(turnDoneMsg{result: res})
	if nm.(Model).planmode.Active {
		t.Fatal("a text-less plan turn must not open the post-plan picker")
	}
}

func TestOnTurnDone_NoPickerOutsidePlanMode(t *testing.T) {
	m := newTestModel(t)
	m.role = "worker"
	m.state = StateStreaming
	res := loop.RunResult{Messages: []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "done"}}},
	}}
	nm, _ := m.onTurnDone(turnDoneMsg{result: res})
	if nm.(Model).planmode.Active {
		t.Fatal("edit-mode turns must not open the post-plan picker")
	}
}
