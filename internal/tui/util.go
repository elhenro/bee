package tui

import (
	"github.com/charmbracelet/x/ansi"
)

// lipglossWidth measures visible cells (strips ANSI sequences before count).
func lipglossWidth(s string) int {
	return len([]rune(ansi.Strip(s)))
}

// truncateVisible cuts a styled string to width visible cells. ANSI runs are
// kept verbatim; this is a coarse pass intended for the strip row only.
func truncateVisible(s string, width int) string {
	if width <= 0 {
		return ""
	}
	stripped := ansi.Strip(s)
	r := []rune(stripped)
	if len(r) <= width {
		return s
	}
	// fallback: re-render plain truncated text — loses styles but stays safe.
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}
