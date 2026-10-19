package tui

import (
	"context"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
)

// doneProvider answers every request with a final line carrying the done
// signal, the way an unattended agent closes a task.
type doneProvider struct {
	mu   sync.Mutex
	reqs int
}

func (p *doneProvider) Name() string { return "stub" }
func (p *doneProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	p.reqs++
	p.mu.Unlock()
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: "all done\n<promise>done</promise>"}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

// turnDoneIn runs cmd (descending into batches) and returns the turnDoneMsg
// the engine goroutine produced.
func turnDoneIn(cmd tea.Cmd) (turnDoneMsg, bool) {
	if cmd == nil {
		return turnDoneMsg{}, false
	}
	switch msg := cmd().(type) {
	case turnDoneMsg:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if done, ok := turnDoneIn(c); ok {
				return done, true
			}
		}
	}
	return turnDoneMsg{}, false
}

// a follow-up queued with alt+enter must fire when the running turn ends on
// the done signal, same as on any other clean finish.
func TestQueue_FiresAfterDoneSignalTurn(t *testing.T) {
	cfg := config.Defaults()
	cfg.Memory.Enabled = false
	prov := &doneProvider{}
	eng := &loop.Engine{Provider: prov, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: t.TempDir()}
	m := NewModel(eng, t.TempDir(), "m", "", caveman.Default)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(Model)

	m.input.SetValue("first")
	nm, cmd := m.handleSubmit()
	m = nm.(Model)
	m.input.SetValue("second")
	nm, _ = m.handleFollowUp()
	m = nm.(Model)
	if len(m.queue) != 1 {
		t.Fatalf("follow-up not queued: %v", m.queue)
	}

	done, ok := turnDoneIn(cmd)
	if !ok || done.err != nil {
		t.Fatalf("first turn: ok=%v err=%v", ok, done.err)
	}
	nm, cmd = m.Update(done)
	m = nm.(Model)
	if len(m.queue) != 0 || m.state != StateStreaming {
		t.Fatalf("queued follow-up did not start after the done turn: queue=%v state=%v", m.queue, m.state)
	}
	done, ok = turnDoneIn(cmd)
	if !ok || done.err != nil {
		t.Fatalf("second turn: ok=%v err=%v", ok, done.err)
	}
	if prov.reqs != 2 {
		t.Fatalf("want 2 model requests (first + queued), got %d", prov.reqs)
	}
	nm, _ = m.Update(done)
	if got := nm.(Model).state; got != StateIdle {
		t.Fatalf("want idle after the queue drained, got %v", got)
	}
}
