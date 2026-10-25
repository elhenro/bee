package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/loop"
)

// a steer typed while the last LLM round streams is never read by the loop;
// cancellation must run it as a follow-up, not park it in SteerCh or m.queue.
func TestSteerLeftover_AfterCancelBecomesFollowUp(t *testing.T) {
	eng := &loop.Engine{SteerCh: make(chan string, 4)}
	m := NewModel(eng, "/tmp/work", "m", "", caveman.Default)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(Model)
	m.state = StateStreaming
	m.turnGen = 1
	m.interrupted = true
	eng.SteerCh <- "also check the tests"

	nm, cmd := m.onTurnDone(turnDoneMsg{gen: 1, err: context.Canceled, result: loop.RunResult{Messages: m.messages}})
	m = nm.(Model)
	if n := len(eng.SteerCh); n != 0 {
		t.Fatalf("%d steer(s) left parked in SteerCh", n)
	}
	if len(m.queue) != 0 || cmd == nil || m.state != StateStreaming {
		t.Fatalf("leftover steer was not resubmitted: queue=%v state=%v", m.queue, m.state)
	}
	last := m.messages[len(m.messages)-1]
	if !strings.Contains(joinText(last), "also check the tests") {
		t.Fatalf("follow-up turn has wrong text: %q", joinText(last))
	}
}

// enter during a running turn means "stop and address this", not a soft steer
// that lets the old task keep making tool calls.
func TestMidTurnSubmit_InterruptsAndRunsFollowUp(t *testing.T) {
	eng := &loop.Engine{SteerCh: make(chan string, 4)}
	m := NewModel(eng, "/tmp/work", "m", "", caveman.Default)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(Model)
	m.state = StateStreaming
	m.turnGen = 1
	cancelled := false
	m.cancelRun = func() { cancelled = true }
	m.input.SetValue("answer my question")

	nm, _ = m.handleInterrupt()
	m = nm.(Model)
	if !cancelled || m.state != StateIdle || !m.interrupted {
		t.Fatalf("mid-turn submit did not interrupt: cancelled=%v state=%v interrupted=%v", cancelled, m.state, m.interrupted)
	}
	if len(m.queue) != 1 || m.queue[0] != "answer my question" {
		t.Fatalf("mid-turn submit not queued: %v", m.queue)
	}

	nm, cmd := m.onTurnDone(turnDoneMsg{gen: 1, err: context.Canceled})
	m = nm.(Model)
	if len(m.queue) != 0 || cmd == nil || m.state != StateStreaming {
		t.Fatalf("follow-up did not start after cancellation: queue=%v state=%v", m.queue, m.state)
	}
	last := m.messages[len(m.messages)-1]
	if got := joinText(last); got != "answer my question" {
		t.Fatalf("follow-up turn has wrong text: %q", got)
	}
}
