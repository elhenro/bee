package tui

import (
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// one message for scrollback:
//
//	▎ your turn                   honey bar on a warm panel
//	⬢ the answer                  marker column, prose hangs under it
//	● Read  path                  tool call, bullet colored by outcome
//	  ⎿  20 lines                 result hangs off its call
//
// blank runs collapse so glamour padding or a stray `\n\n\n` can't spill
// empty rows. consecutive tool messages stack tight so a batch reads as one
// tree; everything else gets a blank above.
func (r *StreamRenderer) RenderMessage(m types.Message) string {
	// synthetic [nudge] turns are hidden unless show_nudges; the loop still
	// injects and persists them
	if !r.showNudges && isNudgeMessage(m) {
		return ""
	}
	// `!cmd` / `!!cmd` records get their own layout
	if tb := firstTextBlock(m); tb != nil {
		if cmd, out, isErr, ok := parseInlineShell(tb.Text); ok {
			r.afterTool = false
			return r.frame(r.renderInlineShell(cmd, out, isErr), false, false)
		}
	}
	// index tool uses so a later result can find its call
	for _, b := range m.Content {
		if b.Type == types.BlockToolUse && b.Use != nil {
			if r.toolUses == nil {
				r.toolUses = make(map[string]types.ToolUse)
			}
			r.toolUses[b.Use.ID] = *b.Use
		}
	}

	switch m.Role {
	case types.RoleUser:
		body := m.Display
		if body == "" {
			body = joinText(m)
		}
		r.afterTool = false
		return r.frame(r.renderUser(body), true, false)
	case types.RoleAssistant:
		if note, ok := noticeText(m); ok {
			r.afterTool = false
			return r.frame(r.renderNotice(note), false, false)
		}
		body := r.assistantBody(m)
		if body == "" {
			return ""
		}
		r.afterTool = false
		return r.frame(body, true, false)
	default:
		r.clipped = false
		body := r.toolBody(m)
		if body == "" {
			return ""
		}
		tight := r.afterTool
		r.afterTool = true
		return r.frame(body, false, tight)
	}
}

// text and thinking blocks. tool calls render paired with their result in
// the tool message, except escalate (its result is suppressed)
func (r *StreamRenderer) assistantBody(m types.Message) string {
	var parts []string
	for _, b := range m.Content {
		var s string
		switch b.Type {
		case types.BlockText:
			if strings.TrimSpace(b.Text) != "" {
				s = r.renderAssistant(r.renderTextAs(b.Text, m.Ephemeral))
				r.SetContinuation(false) // only the first block continues a flushed head
			}
		case types.BlockThinking:
			s = r.renderThinking(b.Text)
		case types.BlockToolUse:
			if b.Use != nil && b.Use.Name == "escalate" {
				s = r.renderToolUse(*b.Use)
			}
		}
		if s = strings.TrimRight(s, "\n"); s != "" {
			parts = append(parts, s)
		}
	}
	return collapseBlankRuns(strings.Trim(strings.Join(parts, "\n\n"), "\n"))
}

// one card per result, stacked tight
func (r *StreamRenderer) toolBody(m types.Message) string {
	var parts []string
	for _, b := range m.Content {
		if b.Type != types.BlockToolResult || b.Result == nil {
			continue
		}
		if s := strings.TrimRight(r.pairedToolResult(*b.Result), "\n"); s != "" {
			parts = append(parts, s)
		}
	}
	return collapseBlankRuns(strings.Trim(strings.Join(parts, "\n"), "\n"))
}

// text blocks as typed
func joinText(m types.Message) string {
	var parts []string
	for _, b := range m.Content {
		if b.Type == types.BlockText {
			parts = append(parts, b.Text)
		}
	}
	return strings.Trim(strings.Join(parts, "\n"), "\n")
}

// spacing layer: gutter, OSC 133 zone marks (shell-integration terminals
// jump between turns), blank row above (tight skips it); compact skips all
func (r *StreamRenderer) frame(s string, zone, tight bool) string {
	if r.compact {
		return s
	}
	s = applyGutter(s)
	if zone {
		s = osc133Start + s + osc133End
	}
	if tight {
		return s
	}
	return "\n" + s
}

// squeezes runs of blank lines to one; glamour and models over-pad and
// every blank in scrollback is a wasted row
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

func (r *StreamRenderer) roleGlyph(role types.Role) string {
	switch role {
	case types.RoleUser:
		return r.styles.RoleYou.Render(glyphUser)
	case types.RoleAssistant:
		return r.styles.Mark.Render(glyphBee)
	case types.RoleTool:
		return ""
	default:
		return r.styles.Dim.Render(glyphSep)
	}
}

// indent every line after the first
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
