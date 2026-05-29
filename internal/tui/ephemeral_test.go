package tui

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// nonEphemeral must drop UI-only echoes (e.g. the "(/new done)" slash
// confirmation) so they never reach the model and get parroted back as a
// bogus finish signal, while keeping real user/assistant turns intact.
func TestNonEphemeral_DropsUIOnlyEchoes(t *testing.T) {
	txt := func(s string) []types.ContentBlock {
		return []types.ContentBlock{{Type: types.BlockText, Text: s}}
	}
	in := []types.Message{
		{Role: types.RoleAssistant, Content: txt("(/new done)"), Ephemeral: true},
		{Role: types.RoleUser, Content: txt("set threshold to 0.2")},
		{Role: types.RoleAssistant, Content: txt("(queued: continue)"), Ephemeral: true},
		{Role: types.RoleAssistant, Content: txt("on it")},
	}

	got := nonEphemeral(in)

	if len(got) != 2 {
		t.Fatalf("expected 2 non-ephemeral messages, got %d: %+v", len(got), got)
	}
	for _, msg := range got {
		if msg.Ephemeral {
			t.Fatalf("ephemeral message leaked through: %+v", msg)
		}
		if txt := msg.Content[0].Text; txt == "(/new done)" || txt == "(queued: continue)" {
			t.Fatalf("UI echo leaked into model context: %q", txt)
		}
	}
	if got[0].Content[0].Text != "set threshold to 0.2" || got[1].Content[0].Text != "on it" {
		t.Fatalf("real turns reordered or altered: %+v", got)
	}
}

func TestNonEphemeral_Empty(t *testing.T) {
	if got := nonEphemeral(nil); len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}
