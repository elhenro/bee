package tui

import (
	"context"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// armedWatchdog builds a streaming model with the watchdog enabled, an already
// stale activity clock, and a prior user instruction to resume. eng is nil so
// submit() takes the echo path — retrigger completes without a real provider.
func armedWatchdog(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.watchdogEnabled = true
	m.watchdogStall = 50 * time.Millisecond
	m.watchdogMaxResumes = 2
	m.state = StateStreaming
	m.messages = []types.Message{{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "finish the task"}},
	}}
	m.lastActivityAt = time.Now().Add(-time.Second) // already stale
	m.cancelRun = func() {}
	return m
}

func TestWatchdog_StallStartsHandshake(t *testing.T) {
	m := armedWatchdog(t)
	out, _ := m.onLoaderTick(loaderTickMsg{})
	got := out.(Model)
	if !got.stallResumePending {
		t.Fatal("expected stallResumePending after a stalled tick")
	}
	if got.cancelRun != nil {
		t.Fatal("expected cancelRun cleared after cancel")
	}
	if got.resumeCount != 0 {
		t.Fatalf("resumeCount should not bump until the cancelled turn lands, got %d", got.resumeCount)
	}
	if got.state != StateStreaming {
		t.Fatalf("state should stay StateStreaming while waiting for cancel, got %v", got.state)
	}
}

func TestWatchdog_StallResumesOnCancelLanding(t *testing.T) {
	m := armedWatchdog(t)
	out, _ := m.onLoaderTick(loaderTickMsg{})
	m = out.(Model)
	// the cancelled in-flight turn now returns context.Canceled.
	out, _ = m.Update(turnDoneMsg{err: context.Canceled})
	got := out.(Model)
	if got.resumeCount != 1 {
		t.Fatalf("resumeCount = %d, want 1 after resume", got.resumeCount)
	}
	if !got.awaitingProgress {
		t.Fatal("expected awaitingProgress after resume")
	}
	if got.state != StateStreaming {
		t.Fatalf("state = %v, want StateStreaming (resumed turn running)", got.state)
	}
	last := got.messages[len(got.messages)-1]
	if last.Role != types.RoleUser || last.Content[0].Text != "finish the task" {
		t.Fatalf("expected the instruction re-sent, got %+v", last)
	}
}

func TestWatchdog_NoStallWhenActive(t *testing.T) {
	m := armedWatchdog(t)
	m.lastActivityAt = time.Now()
	out, cmd := m.onLoaderTick(loaderTickMsg{})
	if out.(Model).stallResumePending {
		t.Fatal("should not stall while activity is fresh")
	}
	if cmd == nil {
		t.Fatal("loader should re-arm when not stalled")
	}
}

func TestWatchdog_ActivityResetsCounterAfterResume(t *testing.T) {
	m := armedWatchdog(t)
	m.resumeCount = 1
	m.awaitingProgress = true
	out, _ := m.Update(streamDeltaMsg{Delta: "working"})
	got := out.(Model)
	if got.resumeCount != 0 || got.awaitingProgress {
		t.Fatalf("progress should reset budget: count=%d awaiting=%v", got.resumeCount, got.awaitingProgress)
	}
}

func TestWatchdog_CapParks(t *testing.T) {
	m := armedWatchdog(t)
	m.resumeCount = 2 // == max
	out, _ := m.onLoaderTick(loaderTickMsg{})
	m = out.(Model)
	if !m.stallResumePending {
		t.Fatal("stall should still cancel the hang at the cap")
	}
	out, _ = m.Update(turnDoneMsg{err: context.Canceled})
	got := out.(Model)
	if got.state != StateError {
		t.Fatalf("state = %v, want StateError at the cap", got.state)
	}
	if got.resumeCount != 2 {
		t.Fatalf("resumeCount should stay at cap, got %d", got.resumeCount)
	}
}

func TestWatchdog_UserTypingSuppressesStall(t *testing.T) {
	m := armedWatchdog(t)
	m.input.SetValue("wait, do this instead")
	out, cmd := m.onLoaderTick(loaderTickMsg{})
	if out.(Model).stallResumePending {
		t.Fatal("must not stall while the user is typing")
	}
	if cmd == nil {
		t.Fatal("loader should still re-arm")
	}
}

func TestWatchdog_DisabledNoStall(t *testing.T) {
	m := armedWatchdog(t)
	m.watchdogDisabled = true
	out, _ := m.onLoaderTick(loaderTickMsg{})
	if out.(Model).stallResumePending {
		t.Fatal("disabled watchdog must not stall")
	}
}

func TestWatchdog_RecoverableErrorSchedulesResume(t *testing.T) {
	m := armedWatchdog(t)
	out, cmd := m.Update(turnDoneMsg{err: context.DeadlineExceeded})
	got := out.(Model)
	if got.state != StateError {
		t.Fatalf("state = %v, want StateError", got.state)
	}
	if cmd == nil {
		t.Fatal("recoverable error should schedule an auto-resume cmd")
	}
}

func TestWatchdog_WedgeErrorSchedulesResume(t *testing.T) {
	// per the user's decision, model-wedges are resumed (with a recovery nudge).
	m := armedWatchdog(t)
	gen := m.resumeErrGen
	out, cmd := m.Update(turnDoneMsg{err: &loop.FormatStrikeError{Streak: 3}})
	got := out.(Model)
	if cmd == nil || got.resumeErrGen == gen {
		t.Fatal("wedge error should arm an auto-resume")
	}
}

func TestWatchdog_UserCancelDoesNotResume(t *testing.T) {
	m := armedWatchdog(t)
	out, _ := m.Update(turnDoneMsg{err: context.Canceled})
	got := out.(Model)
	if got.state != StateIdle {
		t.Fatalf("plain user cancel should land StateIdle, got %v", got.state)
	}
	if got.resumeCount != 0 {
		t.Fatal("user cancel must reset, never resume")
	}
}

func TestWatchdog_EscalateDoesNotResume(t *testing.T) {
	m := armedWatchdog(t)
	gen := m.resumeErrGen
	out, _ := m.Update(turnDoneMsg{err: &loop.EscalateError{Reason: "stuck"}})
	got := out.(Model)
	if got.state != StateIdle {
		t.Fatalf("escalate should land StateIdle (picker), got %v", got.state)
	}
	if got.resumeErrGen != gen {
		t.Fatal("escalate must not arm an auto-resume")
	}
}

func TestWatchdog_StaleErrorResumeDrops(t *testing.T) {
	m := armedWatchdog(t)
	m.state = StateError
	m.resumeErrGen = 5
	out, cmd := m.onResumeAfterError(resumeAfterErrorMsg{gen: 4, continuation: "x", reason: "timeout"})
	if cmd != nil || out.(Model).state != StateError {
		t.Fatal("a superseded error-resume must drop without acting")
	}
}
