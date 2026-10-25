package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// leading cells of the input: chevron on row one, blanks after so
// multi-line input hangs under it
const (
	promptFirst = " " + glyphPrompt + " "
	promptRest  = "   "
	promptShell = " " + glyphShell + " "
	promptWidth = 3
)

// hairline length: full row minus a gutter each side, matching user panels
// and diffs
func (m Model) ruleWidth() int {
	return max(m.width-2, 1)
}

func (m Model) renderRule() string {
	return " " + m.styles.Rule.Render(strings.Repeat(glyphRule, m.ruleWidth()))
}

// pickers, then the input between two hairlines; the bottom one doubles as
// the context gauge when show_context_bar is on
func (m Model) renderBottomBar() string {
	var above strings.Builder
	if m.quitArmed {
		above.WriteString(" " + m.styles.Mark.Render("press ctrl+d again to quit") + "\n")
	}
	n := len(m.pendingImages)
	if len(m.pendingImage) > 0 {
		n++
	}
	if n > 0 {
		above.WriteString(" " + m.styles.Dim.Render(fmt.Sprintf("%d image(s) staged, submit to attach", n)) + "\n")
	}
	if m.palette.Active {
		above.WriteString(m.palette.View() + "\n")
	}
	if m.picker != nil && m.picker.Active() {
		above.WriteString(m.picker.View() + "\n")
	}
	if m.atpicker.Active {
		above.WriteString(m.atpicker.View() + "\n")
	}
	if m.history.Active {
		above.WriteString(m.history.View() + "\n")
	}
	bottom := m.renderRule()
	if m.showContextBar {
		bottom = m.renderContextBar()
	}
	out := above.String() + m.renderRule() + "\n" + m.renderInput() + "\n" + bottom
	if m.showHelp {
		out += "\n" + m.renderHelpLine()
	}
	if !m.compact {
		out = "\n" + out // breathing row between the transcript and the prompt frame
	}
	return out
}

// textarea with mode styling: a `!` lead is a shell command (honey), queen
// glows rainbow. m is a value copy, so the style edits stay in this render
func (m Model) renderInput() string {
	first := promptFirst
	tint := func(c lipgloss.TerminalColor) {
		st := lipgloss.NewStyle().Foreground(c).Bold(true)
		m.input.FocusedStyle.Prompt, m.input.BlurredStyle.Prompt = st, st
		m.input.FocusedStyle.Text, m.input.BlurredStyle.Text = st, st
		m.input.FocusedStyle.CursorLine, m.input.BlurredStyle.CursorLine = st, st
		// textarea caches a *Style at Focus() time, so the value copy still
		// points at the original; re-focus to re-point it (cmd discarded,
		// cursor is static)
		_ = m.input.Focus()
	}
	switch {
	case strings.HasPrefix(m.input.Value(), "!"):
		first = promptShell
		tint(accentHoney)
	case m.role == "queen":
		tint(rainbowColorForFrame(m.glowFrame))
	}
	m.input.SetPromptFunc(promptWidth, func(i int) string {
		if i == 0 {
			return first
		}
		return promptRest
	})
	return m.input.View()
}

// the `?` cheat sheet, wrapped to the terminal width
func (m Model) renderHelpLine() string {
	pairs := [][2]string{
		{"^P", "model"}, {"^R", "history"}, {"←", "agents"},
		{"^H", "hive"}, {"^/", "caveman:" + string(m.caveLvl)}, {"^I", "image"}, {"shift+↵", "newline"},
		{"shift+tab", "role"}, {"alt+y", "yolo"}, {"^O", "expand output"},
		{"^V", "verbose"}, {"esc", "cancel"}, {"?", "hide"},
	}
	var rows [][][2]string
	var cur [][2]string
	w := 0
	for _, p := range pairs {
		pw := lipgloss.Width(p[0]) + 1 + lipgloss.Width(p[1]) + 2
		if len(cur) > 0 && m.width > 0 && w+pw > m.width-2 {
			rows, cur, w = append(rows, cur), nil, 0
		}
		cur, w = append(cur, p), w+pw
	}
	if len(cur) > 0 {
		rows = append(rows, cur)
	}
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = " " + m.hints(r)
	}
	return strings.Join(lines, "\n")
}

// `key label  key label`, keys in the secondary tone, labels faint
func (m Model) hints(pairs [][2]string) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = m.styles.Key.Render(p[0]) + " " + m.styles.Hint.Render(p[1])
	}
	return strings.Join(parts, "  ")
}

// a picker strip owns the keys above the input
func (m Model) pickerOpen() bool {
	return m.palette.Active || m.atpicker.Active || m.history.Active ||
		(m.picker != nil && m.picker.Active())
}
