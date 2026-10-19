package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
)

// blockingShell stands in for a shell tool parked on an approval prompt.
type blockingShell struct{ release chan struct{} }

func (b *blockingShell) Spec() llm.ToolSpec { return llm.ToolSpec{Name: "bash", Description: "fake"} }
func (b *blockingShell) Run(context.Context, map[string]any) (tools.Result, error) {
	<-b.release
	return tools.Result{Content: "done"}, nil
}

// `!cmd` must not run inside Update: the shell can block on an approval modal
// only the update loop can answer, and a slow command would freeze the UI.
func TestBang_DoesNotBlockUpdateAndLandsViaMsg(t *testing.T) {
	sh := &blockingShell{release: make(chan struct{})}
	reg := tools.NewRegistry()
	if err := reg.Register(sh); err != nil {
		t.Fatal(err)
	}
	m := NewModel(&loop.Engine{Tools: reg}, "/tmp/work", "m", "", caveman.Default)
	m.width, m.height = 100, 30
	m.input.SetValue("!sleep 30")

	type ret struct {
		m   tea.Model
		cmd tea.Cmd
	}
	got := make(chan ret, 1)
	go func() {
		nm, cmd := m.handleSubmit()
		got <- ret{nm, cmd}
	}()
	var r ret
	select {
	case r = <-got:
	case <-time.After(2 * time.Second):
		close(sh.release)
		t.Fatal("handleSubmit blocked on the shell tool")
	}
	if r.cmd == nil {
		t.Fatal("expected a cmd that runs the shell")
	}
	msgc := make(chan tea.Msg, 1)
	go func() { msgc <- r.cmd() }()
	close(sh.release)
	var msg tea.Msg
	select {
	case msg = <-msgc:
	case <-time.After(2 * time.Second):
		t.Fatal("shell cmd never finished")
	}
	done, ok := msg.(inlineShellDoneMsg)
	if !ok {
		t.Fatalf("got %T, want inlineShellDoneMsg", msg)
	}
	nm, _ := r.m.Update(done)
	last := nm.(Model).messages
	if len(last) == 0 || !strings.Contains(joinText(last[len(last)-1]), "$ sleep 30") {
		t.Fatalf("shell record not appended: %+v", last)
	}
}
