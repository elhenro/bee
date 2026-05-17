package tui

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestRenderMessage_InlineSingleLine(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	m := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "hey hoo"}},
	}
	out := stripANSI(r.RenderMessage(m))
	t.Logf("plain: %q", out)
	// RenderMessage prepends a Spacer(1) "\n" above every turn; body itself
	// must remain single-line.
	body := strings.TrimLeft(out, "\n")
	if strings.Contains(body, "\n") {
		t.Fatalf("expected single-line render after leading spacer, got newline in: %q", body)
	}
	// user turns render with outer gutter + left rail + glyph: ` ▎ ▸ text`.
	if got := strings.TrimRight(body, " "); got != " ▎ ▸ hey hoo" {
		t.Fatalf("expected %q, got %q", " ▎ ▸ hey hoo", got)
	}
}
