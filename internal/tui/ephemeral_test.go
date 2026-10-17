package tui

import (
	"strings"
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
		{Role: types.RoleAssistant, Content: txt("(steering: continue)"), Ephemeral: true},
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
		if txt := msg.Content[0].Text; txt == "(/new done)" || txt == "(steering: continue)" {
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

// text typed mid-turn echoes as your own turn with a tag for what happens to
// it, and stays out of the history the model sees
func TestUserEcho_RendersAsYourTurnAndStaysOutOfHistory(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	echo := userEcho("also add a test", "steering")
	out := stripANSI(r.RenderMessage(echo))
	for _, want := range []string{glyphUser, "also add a test", "steering"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, glyphBee) {
		t.Fatalf("a typed echo must not wear the assistant marker: %q", out)
	}
	if got := nonEphemeral([]types.Message{echo}); len(got) != 0 {
		t.Fatalf("echo leaked into model history: %+v", got)
	}
}
