package queen

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/zzz"
)

func newTestModel(n int) *Model {
	runs := make([]*zzz.Run, n)
	for i := range runs {
		runs[i] = &zzz.Run{ID: "id", Branch: "zzz/x", Status: "running"}
	}
	return New(runs, func() {})
}

func drain(m *Model) {
	for {
		select {
		case msg := <-m.msgs:
			m.Update(msg)
		default:
			return
		}
	}
}

func TestWorkerUIRoutesByIndex(t *testing.T) {
	m := newTestModel(3)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Worker(1).SetIter(4, 40)
	m.Worker(1).SetPhase("engine.run")
	m.Worker(1).SetTokens(zzz.TokenStat{Input: 2000, Output: 500})
	m.Worker(1).IncCommits()
	drain(m)

	if got := m.workers[1].iter; got != 4 {
		t.Fatalf("bee 1 iter = %d, want 4", got)
	}
	if m.workers[0].iter != 0 {
		t.Fatal("bee 0 must be untouched")
	}
	if m.workers[1].commits != 1 {
		t.Fatalf("bee 1 commits = %d, want 1", m.workers[1].commits)
	}
}

func TestDoneCountsTowardAllDone(t *testing.T) {
	m := newTestModel(2)
	if m.allDone() {
		t.Fatal("not all done yet")
	}
	m.Done(0, &zzz.Run{Status: "completed"}, nil)
	m.Done(1, &zzz.Run{Status: "failed"}, nil)
	drain(m)
	if !m.allDone() {
		t.Fatal("both workers reported, should be allDone")
	}
}

func TestCtrlCStopThenForceCancel(t *testing.T) {
	canceled := false
	runs := []*zzz.Run{{ID: "a", Status: "running"}}
	m := New(runs, func() { canceled = true })

	// first ctrl+c: graceful broadcast, no quit
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Fatal("first ctrl+c should not quit")
	}
	if !m.stopArmed {
		t.Fatal("first ctrl+c should arm stop")
	}
	select {
	case s := <-m.Worker(0).steer:
		if s.Kind != zzz.SteerStop {
			t.Fatalf("want SteerStop broadcast, got %q", s.Kind)
		}
	default:
		t.Fatal("first ctrl+c did not broadcast stop")
	}

	// second ctrl+c: force cancel + quit
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("second ctrl+c should quit")
	}
	if !canceled {
		t.Fatal("second ctrl+c should cancel ctx")
	}
}

func TestCtrlDCancelsAndQuits(t *testing.T) {
	canceled := false
	m := New([]*zzz.Run{{ID: "a", Status: "running"}}, func() { canceled = true })
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if cmd == nil {
		t.Fatal("ctrl+d should quit")
	}
	if !canceled {
		t.Fatal("ctrl+d should cancel ctx")
	}
}

func TestViewRendersBees(t *testing.T) {
	m := newTestModel(2)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := m.View()
	if !strings.Contains(out, "queen") {
		t.Fatalf("view missing header: %q", out)
	}
	if !strings.Contains(out, "bee 01") || !strings.Contains(out, "bee 02") {
		t.Fatalf("view missing bee rows: %q", out)
	}
}
