package tui

import (
	"testing"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
)

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
	av.Refresh()
	rows := av.Rows()
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
	av.Refresh()
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
