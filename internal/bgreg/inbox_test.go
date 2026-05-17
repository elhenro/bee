package bgreg

import "testing"

func TestInboxAppendAndDrain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)
	id := "sess-1"

	msgs, cursor, err := InboxDrain(id, 0)
	if err != nil {
		t.Fatalf("drain empty: %v", err)
	}
	if len(msgs) != 0 || cursor != 0 {
		t.Fatalf("expected no messages, got %d at cursor %d", len(msgs), cursor)
	}

	if err := InboxAppend(id, "first follow-up"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := InboxAppend(id, "second follow-up"); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	msgs, cursor, err = InboxDrain(id, 0)
	if err != nil {
		t.Fatalf("drain after 2: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Text != "first follow-up" || msgs[1].Text != "second follow-up" {
		t.Fatalf("unexpected msgs: %+v", msgs)
	}
	if cursor <= 0 {
		t.Fatalf("cursor must advance, got %d", cursor)
	}

	more, c2, err := InboxDrain(id, cursor)
	if err != nil {
		t.Fatalf("drain again: %v", err)
	}
	if len(more) != 0 || c2 != cursor {
		t.Fatalf("expected idle drain, got %d msgs cursor=%d", len(more), c2)
	}

	if err := InboxAppend(id, "third"); err != nil {
		t.Fatalf("append 3: %v", err)
	}
	final, _, err := InboxDrain(id, cursor)
	if err != nil {
		t.Fatalf("drain after 3: %v", err)
	}
	if len(final) != 1 || final[0].Text != "third" {
		t.Fatalf("unexpected tail: %+v", final)
	}
}
