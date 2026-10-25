package uikit

import "testing"

func TestMailboxDropsWhenFullOrClosed(t *testing.T) {
	b := NewMailbox(1)
	b.Send(1)
	b.Send(2) // full: dropped, must not block
	if got := <-b.C; got != 1 {
		t.Fatalf("got %v, want 1", got)
	}
	b.Close()
	b.Close() // idempotent
	b.Send(3)
	select {
	case v := <-b.C:
		t.Fatalf("closed mailbox delivered %v", v)
	default:
	}
}

func TestMailboxWait(t *testing.T) {
	b := NewMailbox(1)
	b.Send("x")
	if got := b.Wait()(); got != "x" {
		t.Fatalf("Wait = %v", got)
	}
}

func TestLevelFor(t *testing.T) {
	for in, want := range map[string]string{
		"push failed: x":    "err",
		"Error writing":     "err",
		"agent BLOCKED":     "warn",
		"permission denied": "warn",
		"[zzz] noted: hi":   "info",
	} {
		if got := LevelFor(in); got != want {
			t.Errorf("LevelFor(%q) = %q, want %q", in, got, want)
		}
	}
}
