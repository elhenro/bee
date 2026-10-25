package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// paneTitle renders a modal heading in the honey accent.
func paneTitle(s string) string {
	return lipgloss.NewStyle().Foreground(accentHoney).Bold(true).Render(s)
}

// paneRow returns the cursor marker and name style for one list row.
func paneRow(selected bool) (marker string, name lipgloss.Style) {
	name = lipgloss.NewStyle().Foreground(fgOyster)
	if selected {
		return selectMark(true), name.Foreground(accentHoney).Bold(true)
	}
	return "  ", name
}

// HighlightLabel renders label with matched runes accented, then pads the
// visible width to n columns so toggle/desc columns stay aligned.
func HighlightLabel(label string, matched []int, base, hl lipgloss.Style, n int) string {
	hits := map[int]struct{}{}
	for _, idx := range matched {
		if idx >= 0 && idx < len(label) {
			hits[idx] = struct{}{}
		}
	}
	var b strings.Builder
	for i, r := range label {
		ch := string(r)
		if _, ok := hits[i]; ok {
			b.WriteString(hl.Render(ch))
		} else {
			b.WriteString(base.Render(ch))
		}
	}
	pad := n - lipgloss.Width(label)
	if pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	return b.String()
}
