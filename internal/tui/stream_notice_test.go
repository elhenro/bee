package tui

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// an auto-resume is sent as a user turn but the user didn't type it: it must
// render as a quiet notice, not a user bubble
func TestAutoResume_RendersAsNoticeNotUserBubble(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	msg := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: resumeContinueText}},
		Display: "(auto-resume 1/3: stalled)",
	}
	out := stripANSI(r.RenderMessage(msg))
	if !strings.Contains(out, "auto-resume 1/3: stalled") {
		t.Fatalf("missing notice text in %q", out)
	}
	if strings.Contains(out, glyphUser) || strings.Contains(out, "(auto-resume") {
		t.Fatalf("auto-resume must not wear the user bubble or parens: %q", out)
	}
}

// a typed message that merely looks parenthetical keeps its user bubble when
// no Display override is set
func TestUserTurn_ParensWithoutDisplayStaysBubble(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	msg := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "(maybe later)"}},
	}
	if out := stripANSI(r.RenderMessage(msg)); !strings.Contains(out, glyphUser) {
		t.Fatalf("typed turn lost its bubble: %q", out)
	}
}
