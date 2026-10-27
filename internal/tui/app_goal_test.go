package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/goal"
	"github.com/elhenro/bee/internal/types"
)

func TestHandleGoalSetActivatesAndStreams(t *testing.T) {
	m := newTestModel(t)
	out, _ := m.handleGoal([]string{"make", "the", "build", "pass"})
	got, ok := out.(Model)
	if !ok {
		t.Fatalf("handleGoal returned %T, want Model", out)
	}
	if !got.goal.Active {
		t.Fatalf("goal not active after set")
	}
	if got.goal.Condition != "make the build pass" {
		t.Fatalf("condition = %q, want %q", got.goal.Condition, "make the build pass")
	}
	if got.state != StateStreaming {
		t.Fatalf("state = %v, want StateStreaming", got.state)
	}
}

func TestHandleGoalClearDeactivates(t *testing.T) {
	m := newTestModel(t)
	m.goal.Set("fix the lint", 0, time.Now())
	if !m.goal.Active {
		t.Fatalf("precondition: goal should be active")
	}
	out, _ := m.handleGoal([]string{"clear"})
	got, ok := out.(Model)
	if !ok {
		t.Fatalf("handleGoal returned %T, want Model", out)
	}
	if got.goal.Active {
		t.Fatalf("goal still active after clear")
	}
}

func TestGoalShowDoesNotActivate(t *testing.T) {
	m := newTestModel(t)
	out, _ := m.handleGoal([]string{"show"})
	got := out.(Model)
	if got.goal.Active {
		t.Fatalf("show should not activate a goal")
	}
}

// the same not-met reply three turns running stops the goal with a typed
// no-progress reason instead of spinning to the turn cap.
func TestGoalStopsOnIdenticalReplies(t *testing.T) {
	m := newTestModel(t)
	m.goal.Set("fix the lint", 0, time.Now())
	m.goalGen = 1
	reply := types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "still on it"}}}
	m.messages = append(m.messages, reply)
	notMet := goalEvalDoneMsg{gen: 1, v: goal.Verdict{Reason: "lint still failing"}}
	for i := range goal.MaxIdenticalReplies - 1 {
		out, _ := m.onGoalEvalDone(notMet)
		m = out.(Model)
		if !m.goal.Active {
			t.Fatalf("goal stopped early at turn %d", i+1)
		}
		m.goalGen = 1
		m.state = StateIdle // the continuation turn finished
	}
	out, _ := m.onGoalEvalDone(notMet)
	got := out.(Model)
	if got.goal.Active {
		t.Fatal("goal still active after identical-reply streak")
	}
	last := got.messages[len(got.messages)-1]
	text := strings.Trim(last.Content[0].Text, "()")
	if !last.Ephemeral || !strings.HasPrefix(text, goal.StoppedPrefix) || !strings.Contains(text, "identical replies") {
		t.Fatalf("stop message = %+v", last)
	}
}

// setting a goal mid-turn used to start a second submit that dropped the
// running turn's cancel func; show/clear must keep working during a run.
func TestHandleGoalSetWhileStreamingIsRefused(t *testing.T) {
	m := newTestModel(t)
	m.state = StateStreaming
	m.turnGen = 3
	cancelled := false
	m.cancelRun = func() { cancelled = true }
	m.input.SetValue("/goal ship it")
	out, _ := m.handleSubmit()
	got := out.(Model)
	if got.goal.Active || got.turnGen != 3 || cancelled {
		t.Fatalf("goal set mid-run: active=%v turnGen=%d cancelled=%v", got.goal.Active, got.turnGen, cancelled)
	}
	if got.cancelRun == nil || got.state != StateStreaming {
		t.Fatalf("running turn was disturbed: state=%v", got.state)
	}
	last := got.messages[len(got.messages)-1]
	if !last.Ephemeral || !strings.Contains(joinText(last), "idle") {
		t.Fatalf("no refusal notice: %+v", last)
	}

	got.goal.Set("fix lint", 0, time.Now())
	got.input.SetValue("/goal clear")
	out, _ = got.handleSubmit()
	if out.(Model).goal.Active {
		t.Fatalf("/goal clear must work during a run")
	}
}
