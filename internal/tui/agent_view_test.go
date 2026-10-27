package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/bgreg"
)

// loadAgentRows runs the view's refresh the way the program does: the cmd
// reads the registry, and the result goes back through Update.
func loadAgentRows(t *testing.T, av *AgentView) tea.Cmd {
	t.Helper()
	msg, ok := av.refreshCmd()().(agentRowsMsg)
	if !ok {
		t.Fatal("refresh cmd must yield an agentRowsMsg")
	}
	_, next := av.Update(msg)
	return next
}

func TestAgentViewLoadsStatuses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)

	for _, st := range []bgreg.Status{
		{SessionID: "a", State: bgreg.StateAwaiting, Task: "auth refactor", LastResponse: "splitting flow.go", UpdatedAt: time.Now()},
		{SessionID: "b", State: bgreg.StateActive, Task: "agent view", UpdatedAt: time.Now()},
	} {
		_ = bgreg.Write(st)
	}

	av := NewAgentView()
	av.Open()
	loadAgentRows(t, av)
	rows := av.rows
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	awaiting := false
	for _, r := range rows {
		if r.State == bgreg.StateAwaiting {
			awaiting = true
		}
	}
	if !awaiting {
		t.Fatalf("expected an awaiting row, got %+v", rows)
	}
}

func TestAgentViewReplyWritesInbox(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)
	_ = bgreg.Write(bgreg.Status{SessionID: "a", State: bgreg.StateAwaiting, UpdatedAt: time.Now()})

	av := NewAgentView()
	av.Open()
	loadAgentRows(t, av)
	av.selected = 0
	if err := av.SubmitReply("ok ship it"); err != nil {
		t.Fatalf("SubmitReply: %v", err)
	}
	msgs, _, err := bgreg.InboxDrain("a", 0)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Text != "ok ship it" {
		t.Fatalf("unexpected inbox: %+v", msgs)
	}
}

// a refresh result schedules exactly one next tick, and the tick asks for
// exactly one refresh: the chain never fans out.
func TestAgentView_ChainIsLinear(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	av := NewAgentView()
	av.Open()
	if av.start() == nil {
		t.Fatal("first start must arm the chain")
	}
	if av.start() != nil {
		t.Fatal("start must arm once per open")
	}
	if next := loadAgentRows(t, av); next == nil {
		t.Fatal("a refresh result must schedule the next tick")
	}
	if _, cmd := av.Update(agentTickMsg{gen: av.gen}); cmd == nil {
		t.Fatal("a live tick must trigger a refresh")
	}
}

// close and reopen inside the tick interval: the first open's pending tick and
// refresh must die instead of running beside the second open's chain.
func TestAgentView_ReopenDropsOldChain(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	av := NewAgentView()
	av.Open()
	av.start()
	oldGen := av.gen
	av.Close()
	av.Open()
	if av.start() == nil {
		t.Fatal("reopen must arm a fresh chain")
	}
	if _, cmd := av.Update(agentTickMsg{gen: oldGen}); cmd != nil {
		t.Error("tick from the previous open must be dropped")
	}
	if _, cmd := av.Update(agentRowsMsg{gen: oldGen}); cmd != nil {
		t.Error("refresh result from the previous open must be dropped")
	}
}

// a view opened without a cmd of its own (the /agent slash command) gets its
// chain armed by the next Update.
func TestModel_ArmsAgentViewOpenedBySlash(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	m := newTestModel(t)
	if err := m.side().OpenAgentView(); err != nil {
		t.Fatal(err)
	}
	nm, cmd := m.Update(warningFadeMsg{})
	if cmd == nil {
		t.Fatal("update after open must arm the refresh chain")
	}
	if _, ok := cmd().(agentRowsMsg); !ok {
		t.Fatal("armed cmd must be the first refresh")
	}
	if _, cmd := nm.(Model).Update(warningFadeMsg{}); cmd != nil {
		t.Fatal("chain must be armed once, not on every update")
	}
}
