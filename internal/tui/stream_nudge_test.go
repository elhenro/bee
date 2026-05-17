package tui

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

// nudge messages are synthetic user turns the loop appends to recover from
// reasoning-only stalls. they pollute scrollback for the user but the
// provider must still see them — hence a render-only filter.
func nudgeMessage() types.Message {
	return types.Message{
		Role: types.RoleUser,
		Content: []types.ContentBlock{{
			Type: types.BlockText,
			Text: "[nudge] previous turn was reasoning-only. respond now: emit final answer or call a tool.",
		}},
	}
}

func plainUserMessage() types.Message {
	return types.Message{
		Role: types.RoleUser,
		Content: []types.ContentBlock{{
			Type: types.BlockText,
			Text: "hello bee",
		}},
	}
}

// default: show_nudges=false hides the [nudge] row from scrollback.
func TestRenderMessage_NudgeHiddenByDefault(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	out := r.RenderMessage(nudgeMessage())
	if out != "" {
		t.Fatalf("nudge message must render empty when show_nudges=false, got %q", out)
	}
}

// show_nudges=true reveals the nudge row.
func TestRenderMessage_NudgeShownWhenEnabled(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetShowNudges(true)
	out := stripANSI(r.RenderMessage(nudgeMessage()))
	if !strings.Contains(out, "[nudge]") {
		t.Fatalf("expected nudge marker in output when shown, got %q", out)
	}
	if !strings.Contains(out, "reasoning-only") {
		t.Fatalf("expected nudge body in output, got %q", out)
	}
}

// non-nudge user messages always render regardless of the flag.
func TestRenderMessage_PlainUserAlwaysRenders(t *testing.T) {
	for _, show := range []bool{false, true} {
		r := NewStreamRenderer(DefaultStyles(), 80)
		r.SetShowNudges(show)
		out := stripANSI(r.RenderMessage(plainUserMessage()))
		if !strings.Contains(out, "hello bee") {
			t.Fatalf("plain user message must render with show_nudges=%v, got %q", show, out)
		}
	}
}

// nudge prefix detection must not match prose that happens to mention the
// word "nudge" elsewhere — only the literal "[nudge]" prefix counts.
func TestIsNudgeMessage_PrefixOnly(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"[nudge] go", true},
		{"  [nudge] leading whitespace ok", true},
		{"please nudge me later", false},
		{"prefix [nudge] mid-line", false},
		{"", false},
	}
	for _, c := range cases {
		m := types.Message{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: c.text}},
		}
		if got := isNudgeMessage(m); got != c.want {
			t.Errorf("isNudgeMessage(%q) = %v want %v", c.text, got, c.want)
		}
	}
	// assistant role with [nudge] prefix must not count — only user turns
	// are synthetic recovery prods.
	asst := types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "[nudge] not a recovery"}},
	}
	if isNudgeMessage(asst) {
		t.Errorf("isNudgeMessage(assistant) should be false")
	}
}
