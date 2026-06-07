package tui

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// Regression: after esc cancels turn A and the user resubmits (turn B), A's
// in-flight goroutine returns context.Canceled and emits a late turnDoneMsg.
// It carries A's stale epoch, so onTurnDone must ignore it — nilling cancelRun
// would break esc for B, flipping state to idle would collapse B's live region
// while B keeps generating, and overwriting messages would drop B's user turn.
func TestModel_StaleTurnDoneMsg_DoesNotClobberLiveTurn(t *testing.T) {
	m := newTestModel(t)
	// turn B is live: submit() bumped the epoch to 2 and armed cancelB.
	m.turnGen = 2
	m.state = StateStreaming
	cancelledB := false
	m.cancelRun = func() { cancelledB = true }
	m.messages = []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "turn B prompt"}},
	}}

	// turn A's late cancelled result lands, tagged with the stale epoch 1.
	out, cmd := m.Update(turnDoneMsg{gen: 1, err: context.Canceled, result: loop.RunResult{
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "turn A stale"}},
		}},
	}})
	got := out.(Model)

	if cmd != nil {
		t.Fatal("stale turnDoneMsg should be a no-op (nil cmd)")
	}
	if got.state != StateStreaming {
		t.Fatalf("stale cancel must not flip the live turn to idle; got %v", got.state)
	}
	if got.cancelRun == nil {
		t.Fatal("stale cancel must not nil cancelRun — esc would break for turn B")
	}
	got.cancelRun()
	if !cancelledB {
		t.Fatal("cancelRun must still point at cancelB after the stale msg")
	}
	if len(got.messages) != 1 || got.messages[0].Content[0].Text != "turn B prompt" {
		t.Fatalf("stale result must not clobber B's messages; got %+v", got.messages)
	}
}

// The current turn's result (epoch matches) is still handled normally.
func TestModel_MatchingTurnDoneMsg_Processed(t *testing.T) {
	m := newTestModel(t)
	m.turnGen = 2
	m.state = StateStreaming
	m.cancelRun = func() {}

	out, _ := m.Update(turnDoneMsg{gen: 2, err: context.Canceled})
	got := out.(Model)
	if got.state != StateIdle {
		t.Fatalf("matching cancel should land idle; got %v", got.state)
	}
}

// A resubmit's engine goroutine must wait for the prior (esc'd) run to fully
// return before touching the shared engine: the second submit captures the
// first's runDone as prevDone and installs a fresh runDone.
func TestModel_Submit_ChainsRunDone(t *testing.T) {
	m := newTestModel(t) // eng == nil → echo path still sets up the epoch + runDone

	out, _ := m.submit("first")
	m1 := out.(Model)
	if m1.runDone == nil {
		t.Fatal("submit must install a runDone channel")
	}
	if m1.turnGen != 1 {
		t.Fatalf("first submit should bump turnGen to 1, got %d", m1.turnGen)
	}

	out, _ = m1.submit("second")
	m2 := out.(Model)
	if m2.runDone == m1.runDone {
		t.Fatal("second submit must install a fresh runDone, not reuse the prior")
	}
	if m2.turnGen != 2 {
		t.Fatalf("second submit should bump turnGen to 2, got %d", m2.turnGen)
	}
}
