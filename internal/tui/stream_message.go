package tui

import (
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// RenderMessage formats a single message for the scrollback. Glyph and the
// first body line share one row; continuation lines indent under the body
// column. Dense by design — `▸ yo yo` reads as one unit.
//
// Each ContentBlock renders independently; results are right-trimmed and
// joined on a single newline so neighbouring blocks never collide on the
// same row (the original concat-without-separator caused text to share a
// line with the following tool card). Interior blank runs are collapsed
// so glamour padding or stray `\n\n\n` from the model can't spill
// hundreds of empty rows into terminal scrollback.
func (r *StreamRenderer) RenderMessage(m types.Message) string {
	// hide synthetic [nudge] user messages unless explicitly enabled. the
	// loop still injects + persists these turns; we only suppress the visual
	// row so the user isn't distracted by recovery prods.
	if !r.showNudges && isNudgeMessage(m) {
		return ""
	}
	// inline shell records (from `!cmd` / `!!cmd`) get a dedicated styled
	// rendering instead of the role-glyph + prose layout.
	if tb := firstTextBlock(m); tb != nil {
		if cmd, out, isErr, ok := parseInlineShell(tb.Text); ok {
			body := r.renderInlineShell(cmd, out, isErr)
			if r.compact {
				return body
			}
			return "\n" + applyGutter(body)
		}
	}
	glyph := r.roleGlyph(m.Role)

	// pre-pass: index tool-use blocks so a later renderToolResult can look up
	// the originating command (used by the bash error-card path).
	for _, b := range m.Content {
		if b.Type == types.BlockToolUse && b.Use != nil {
			if r.toolUses == nil {
				r.toolUses = make(map[string]types.ToolUse)
			}
			r.toolUses[b.Use.ID] = *b.Use
		}
	}

	parts := make([]string, 0, len(m.Content))
	for _, b := range m.Content {
		var rendered string
		switch b.Type {
		case types.BlockText:
			rendered = r.renderText(b.Text)
		case types.BlockThinking:
			rendered = r.renderThinking(b.Text)
		case types.BlockToolUse:
			if b.Use != nil {
				rendered = r.renderToolUse(*b.Use)
			}
		case types.BlockToolResult:
			if b.Result != nil {
				rendered = r.renderToolResult(*b.Result)
			}
		}
		rendered = strings.TrimRight(rendered, "\n")
		if rendered == "" {
			continue
		}
		parts = append(parts, rendered)
	}

	bodyStr := collapseBlankRuns(strings.Trim(strings.Join(parts, "\n"), "\n"))

	var rendered string
	if m.Role == types.RoleUser {
		rail := r.styles.UserRail.Render("┃")
		// drop the role glyph: rail + bold-blue body is enough to anchor
		// the turn and reads like a quoted prompt. body color matches the
		// rail so the whole block reads as one unit.
		bodyDecorate := func(s string) string { return r.styles.UserBody.Render(s) }
		if bodyStr == "" {
			rendered = rail
		} else {
			bodyLines := strings.Split(bodyStr, "\n")
			bodyLines[0] = rail + " " + bodyDecorate(bodyLines[0])
			for i := 1; i < len(bodyLines); i++ {
				bodyLines[i] = rail + " " + bodyDecorate(bodyLines[i])
			}
			rendered = strings.Join(bodyLines, "\n")
		}
	} else if m.Role == types.RoleAssistant {
		// assistant turns render without a role glyph — the user prompt
		// above already anchors the conversation, and stripping the prefix
		// gives prose full column width without a leading hex distraction.
		if bodyStr == "" {
			return ""
		}
		rendered = bodyStr
	} else if bodyStr == "" {
		rendered = glyph
	} else if glyph == "" {
		rendered = bodyStr
	} else {
		rendered = glyph + " " + indentContinuation(bodyStr, "  ")
	}

	// compact mode skips the spacing layer entirely (no gutter, no OSC
	// 133 zones, no leading blank). Useful on small terminals or for users
	// who prefer a denser layout.
	if r.compact {
		return rendered
	}

	rendered = applyGutter(rendered)

	// OSC 133 wraps finalized user/assistant turns. Tool messages stay
	// un-wrapped — they're sub-content the terminal shouldn't bookmark.
	if m.Role == types.RoleUser || m.Role == types.RoleAssistant {
		rendered = osc133Start + rendered + osc133End
	}

	// Leading blank line above every message — `Spacer(1)` so each
	// turn breathes vertically. tea.Println adds the trailing \n.
	return "\n" + rendered
}

// collapseBlankRuns squeezes 2+ consecutive blank/whitespace-only lines into
// a single blank. Markdown rendering (glamour) and the model itself both
// like to pad with multiple blanks; in scrollback every blank is a wasted
// row, so cap the run at one. A line is "blank" iff every rune is space or
// tab (after ANSI strip via the surrounding pipeline). Preserves content
// lines verbatim — only whitespace runs get touched.
func collapseBlankRuns(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		isBlank := strings.TrimSpace(l) == ""
		if isBlank && blank {
			continue
		}
		out = append(out, l)
		blank = isBlank
	}
	return strings.Join(out, "\n")
}

// roleGlyph returns the single-glyph role marker. Label dropped — colored
// glyph alone disambiguates speaker without eating a row.
func (r *StreamRenderer) roleGlyph(role types.Role) string {
	switch role {
	case types.RoleUser:
		return r.styles.RoleYou.Render("▸")
	case types.RoleAssistant:
		return r.styles.RoleBee.Render("⬢")
	case types.RoleTool:
		return ""
	default:
		return r.styles.Dim.Render("·")
	}
}

// indentContinuation leaves the first line as-is and prefixes every later
// line with indent. Used so multi-line bodies align under the body column,
// not the role glyph.
func indentContinuation(s, indent string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}
