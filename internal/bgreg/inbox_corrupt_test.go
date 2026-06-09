package bgreg

import (
	"os"
	"testing"
)

// a corrupt line (crash artifact) must advance the cursor like any other
// complete line — otherwise the drain loop re-reads the same offset forever
// and every later message lands misaligned.
func TestInboxDrain_CorruptLineAdvances(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)
	id := "sess-corrupt"

	if err := InboxAppend(id, "before"); err != nil {
		t.Fatalf("append: %v", err)
	}
	p, err := inboxPath(id)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString("{not json\n"); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	_ = f.Close()
	if err := InboxAppend(id, "after"); err != nil {
		t.Fatalf("append after: %v", err)
	}

	msgs, cursor, err := InboxDrain(id, 0)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Text != "before" || msgs[1].Text != "after" {
		t.Fatalf("unexpected msgs: %+v", msgs)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if cursor != fi.Size() {
		t.Fatalf("cursor %d must cover whole file %d (corrupt line included)", cursor, fi.Size())
	}

	more, c2, err := InboxDrain(id, cursor)
	if err != nil {
		t.Fatalf("idle drain: %v", err)
	}
	if len(more) != 0 || c2 != cursor {
		t.Fatalf("idle drain must be empty and stable, got %d msgs cursor=%d", len(more), c2)
	}
}

// a partial trailing line without newline (writer mid-crash) must not be
// consumed: the cursor stays before it so a finished write is picked up later.
func TestInboxDrain_PartialTailNotConsumed(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)
	id := "sess-partial"

	if err := InboxAppend(id, "whole"); err != nil {
		t.Fatalf("append: %v", err)
	}
	p, _ := inboxPath(id)
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if _, err := f.WriteString(`{"text":"par`); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	_ = f.Close()

	msgs, cursor, err := InboxDrain(id, 0)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Text != "whole" {
		t.Fatalf("unexpected msgs: %+v", msgs)
	}

	// complete the partial line; the next drain from cursor must see it whole.
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if _, err := f.WriteString("tial\",\"created_at\":\"2026-01-01T00:00:00Z\"}\n"); err != nil {
		t.Fatalf("complete line: %v", err)
	}
	_ = f.Close()

	more, _, err := InboxDrain(id, cursor)
	if err != nil {
		t.Fatalf("drain 2: %v", err)
	}
	if len(more) != 1 || more[0].Text != "partial" {
		t.Fatalf("expected completed line, got: %+v", more)
	}
}
