package loop

import (
	"testing"
	"time"

	"github.com/elhenro/bee/internal/types"
)

func TestEmitCompactNoticeAppendsEphemeralCard(t *testing.T) {
	e := &Engine{LiveMsgCh: make(chan types.Message, 1)}
	msgs := []types.Message{mkMsg(types.RoleUser, "hi")}
	stats := CompactStats{BeforeMsgs: 10, AfterMsgs: 3, BeforeTokens: 50000, AfterTokens: 8000, Duration: 1200 * time.Millisecond}

	out := e.emitCompactNotice(msgs, stats)
	if len(out) != 2 {
		t.Fatalf("want 2 msgs, got %d", len(out))
	}
	note := out[1]
	if !note.Ephemeral {
		t.Error("notice must be ephemeral")
	}
	if note.Role != types.RoleAssistant {
		t.Errorf("role = %v, want assistant", note.Role)
	}
	if note.Content[0].Text == "" {
		t.Error("notice text empty")
	}
	select {
	case live := <-e.LiveMsgCh:
		if live.ID != note.ID {
			t.Error("live notice id mismatch")
		}
	default:
		t.Error("notice not pushed to LiveMsgCh")
	}
}

func TestEmitCompactNoticeNoopWhenUnchanged(t *testing.T) {
	e := &Engine{}
	msgs := []types.Message{mkMsg(types.RoleUser, "hi")}
	out := e.emitCompactNotice(msgs, CompactStats{BeforeMsgs: 3, AfterMsgs: 3})
	if len(out) != 1 {
		t.Fatalf("want unchanged, got %d msgs", len(out))
	}
}

func TestDropEphemeral(t *testing.T) {
	note := types.Message{Role: types.RoleAssistant, Ephemeral: true,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "card"}}}
	msgs := []types.Message{mkMsg(types.RoleUser, "a"), note, mkMsg(types.RoleAssistant, "b")}

	out := dropEphemeral(msgs)
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	for _, m := range out {
		if m.Ephemeral {
			t.Error("ephemeral survived filter")
		}
	}
	// no ephemeral → same slice, no copy
	clean := []types.Message{mkMsg(types.RoleUser, "x")}
	if got := dropEphemeral(clean); len(got) != 1 {
		t.Errorf("clean slice altered: %d", len(got))
	}
}

func TestLastIDSkipsEphemeral(t *testing.T) {
	real := types.Message{ID: "real", Role: types.RoleAssistant}
	note := types.Message{ID: "note", Role: types.RoleAssistant, Ephemeral: true}
	if got := lastID([]types.Message{real, note}); got != "real" {
		t.Errorf("lastID = %q, want real", got)
	}
}
