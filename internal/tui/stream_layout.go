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

// cell budget for text starting indent cells in, between the gutter and a
// one-cell right margin; floors at 20 so tiny terminals still wrap
func (r *StreamRenderer) textWidth(indent int) int {
	w := r.width - indent - 1
	if !r.compact {
		w--
	}
	if w < 20 {
		w = 20
	}
	return w
}

// word-wrap, breaking inside a word only when one token exceeds the line
func wrapText(s string, w int) string {
	if w <= 0 || s == "" {
		return s
	}
	return ansi.Wrap(s, w, "")
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
