package tui

import (
	"testing"
	"time"
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
