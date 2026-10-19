package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/loop"
)

// a steer typed while the last LLM round streams is never read by the loop;
// it must not stay parked in SteerCh for a later turn to inject.
func TestSteerLeftover_BecomesFollowUpNotNextTurnLeak(t *testing.T) {
	eng := &loop.Engine{SteerCh: make(chan string, 4)}
	m := NewModel(eng, "/tmp/work", "m", "", caveman.Default)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(Model)
	m.state = StateStreaming
	m.turnGen = 1
	m.input.SetValue("also check the tests")
	nm, _ = m.handleSteer()
	m = nm.(Model)

	nm, cmd := m.onTurnDone(turnDoneMsg{gen: 1, result: loop.RunResult{Messages: m.messages}})
	m = nm.(Model)
	if n := len(eng.SteerCh); n != 0 {
		t.Fatalf("%d steer(s) left parked in SteerCh", n)
	}
	if cmd == nil || m.state != StateStreaming {
		t.Fatalf("leftover steer was not resubmitted as a follow-up (state=%v)", m.state)
	}
	last := m.messages[len(m.messages)-1]
	if !strings.Contains(joinText(last), "also check the tests") {
		t.Fatalf("follow-up turn has wrong text: %q", joinText(last))
	}
}
