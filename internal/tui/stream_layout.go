package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// width of the "  ⎿  " connector; continuation rows indent by the same amount
const resultIndent = 5

// width of the marker column ("⬢ ", "✻ ") before prose and reasoning
const markIndent = 2

// narrowest terminal the transcript lays out for; below it rows spill past the
// edge and the terminal wraps them
const minRenderWidth = 20

// smallest text column a block gets, so wrapping never sees a zero width
const minTextWidth = 8

// cell budget for text starting indent cells in, between the gutter and a
// one-cell right margin
func (r *StreamRenderer) textWidth(indent int) int {
	w := r.width - indent - 1
	if !r.compact {
		w--
	}
	return max(w, minTextWidth)
}

// word-wrap, breaking inside a word only when one token exceeds the line
func wrapText(s string, w int) string {
	if w <= 0 || s == "" {
		return s
	}
	return ansi.Wrap(s, w, "")
}

// breaks rows wider than w. glamour wraps at spaces, so a word that outruns
// the line (a URL, a long identifier) would otherwise leave the terminal to
// wrap it
func clampRows(s string, w int) string {
	if w <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if lipgloss.Width(ln) > w {
			lines[i] = ansi.Hardwrap(ln, w, true)
		}
	}
	return strings.Join(lines, "\n")
}

// word-wrap for app-generated text (slash output, status tables). a
// `name  description` row hangs the description under its gap, or drops it to
// its own line when the terminal is too narrow for two columns; any other row
// hangs under its own indent
func wrapColumns(s string, w int) string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		out = append(out, wrapRow(ln, w)...)
	}
	return strings.Join(out, "\n")
}

func wrapRow(ln string, w int) []string {
	if w <= 0 || lipgloss.Width(ln) <= w {
		return []string{ln}
	}
	body := strings.TrimLeft(ln, " ")
	lead := len(ln) - len(body)
	name, desc := body, ""
	if i := strings.Index(body, "  "); i > 0 {
		name, desc = body[:i], strings.TrimLeft(body[i:], " ")
	}
	indent := strings.Repeat(" ", lead)
	if desc == "" {
		return prefixRows(wrapText(body, w-lead), indent, indent)
	}
	hang := lipgloss.Width(ln) - lipgloss.Width(desc)
	if w-hang >= max(16, w/2) {
		return prefixRows(wrapText(desc, w-hang), ln[:len(ln)-len(desc)], strings.Repeat(" ", hang))
	}
	rows := prefixRows(wrapText(name, w-lead), indent, indent)
	return append(rows, prefixRows(wrapText(desc, w-lead-2), indent+"  ", indent+"  ")...)
}

// rows of s, the first behind first and the rest behind rest
func prefixRows(s, first, rest string) []string {
	rows := strings.Split(s, "\n")
	for i := range rows {
		if i == 0 {
			rows[i] = first + rows[i]
		} else {
			rows[i] = rest + rows[i]
		}
	}
	return rows
}

// first line gets first, later non-empty lines get rest
func hang(s, first, rest string) string {
	if s == "" {
		return first
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = first + lines[i]
		} else if lines[i] != "" {
			lines[i] = rest + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// right-pad to w visible cells, never truncates
func padTo(s string, w int) string {
	if n := w - lipgloss.Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// truncate with an ellipsis so a long row can't wrap and tear the left edge
func clipLine(s string, w int) string {
	if w <= 1 || lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, glyphMore)
}

// outer gutter, unless compact
func (r *StreamRenderer) gutter(s string) string {
	if r.compact {
		return s
	}
	return applyGutter(s)
}
