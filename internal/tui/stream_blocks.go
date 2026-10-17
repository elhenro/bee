package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// your turn: honey bar on a warm panel running to the right edge; compact
// drops the panel
func (r *StreamRenderer) renderUser(body string) string {
	rail := lipgloss.NewStyle().Foreground(accentHoney)
	text := r.styles.Body
	if !r.compact {
		rail = r.styles.UserRail
		text = r.styles.UserBody
	}
	if body == "" {
		return rail.Render(glyphUser)
	}
	w := r.textWidth(markIndent + 1)
	lines := strings.Split(wrapText(expandTabs(body), w), "\n")
	for i, ln := range lines {
		if r.compact {
			lines[i] = rail.Render(glyphUser) + " " + text.Render(ln)
			continue
		}
		lines[i] = rail.Render(glyphUser) + text.Render(" "+padTo(ln, w)+" ")
	}
	return strings.Join(lines, "\n")
}

// body is already styled and wrapped to textWidth(markIndent); mid-reply
// (progressive flush printed the marker) it hangs under blank space
func (r *StreamRenderer) renderAssistant(body string) string {
	first := r.styles.Mark.Render(glyphBee) + " "
	if r.cont {
		first = "  "
	}
	return hang(body, first, "  ")
}

// live variant of renderThinking, gutter-applied to match scrollback
func (r *StreamRenderer) RenderThinkingPartial(s string) string {
	body := r.renderThinking(s)
	if body == "" {
		return body
	}
	return r.gutter(body)
}

// faint italic reasoning under a ✻; stripped like tool output so escapes
// can't blit over the chrome
func (r *StreamRenderer) renderThinking(s string) string {
	if !r.showThoughts {
		return ""
	}
	clean := expandTabs(ansi.Strip(strings.TrimRight(s, "\n")))
	if clean == "" {
		return ""
	}
	var rows []string
	for _, ln := range strings.Split(clean, "\n") {
		ln = sanitizeControl(ln)
		if strings.TrimSpace(ln) == "" {
			continue
		}
		rows = append(rows, wrapText(ln, r.textWidth(markIndent)))
	}
	if len(rows) == 0 {
		return ""
	}
	// style per line: SGR must not bleed across the hanging indent.
	lines := strings.Split(strings.Join(rows, "\n"), "\n")
	for i := range lines {
		lines[i] = r.styles.Thought.Render(lines[i])
	}
	return hang(strings.Join(lines, "\n"), r.styles.Dim.Render(glyphThink)+" ", "  ") + "\n"
}
