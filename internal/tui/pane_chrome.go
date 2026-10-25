package tui

import "github.com/charmbracelet/lipgloss"

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
