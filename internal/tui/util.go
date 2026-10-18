package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// lipglossWidth measures visible cells (strips ANSI sequences before count).
func lipglossWidth(s string) int {
	return lipgloss.Width(s)
}

// truncateVisible cuts a styled string to width visible cells, ANSI-aware
// and cell-aware (wide runes count two). Ends in an ellipsis when cut.
func truncateVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, "…")
}
