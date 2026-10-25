// Package tui implements the bubbletea-driven interactive UI for bee.
//
// Visual primitives live here: hex glyphs and the honey palette. Slice 3A
// composes Hive + Workspace via NewHive / NewWorkspace factories.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Hex glyphs (PLAN §8b.1).
const (
	HexFilled = "⬢"
	HexHollow = "⬢"
)

// Pre-built styles. Public so 3A may compose without redefining.
var (
	StyleActive   = lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	StyleAwaiting = lipgloss.NewStyle().Foreground(semWarning).Bold(true)
	StyleIdle     = lipgloss.NewStyle().Foreground(accentBee)
	StyleDone     = lipgloss.NewStyle().Foreground(fgOyster)
	StyleFailed   = lipgloss.NewStyle().Foreground(semError).Bold(true)
	StyleLabel    = lipgloss.NewStyle().Foreground(fgSquid)
)

// HexRow renders a strip of hexagons + names, capped to width cells.
func HexRow(states []BeeState, names []string, width int) string {
	if len(states) == 0 {
		return StyleLabel.Render("(no bees)")
	}
	var parts []string
	for i, st := range states {
		name := ""
		if i < len(names) {
			name = names[i]
		}
		parts = append(parts, renderCell(st, name))
	}
	out := strings.Join(parts, "  ")
	if width > 0 && lipglossWidth(out) > width {
		out = truncateVisible(out, width)
	}
	return out
}

func renderCell(st BeeState, name string) string {
	style, glyph := StyleActive, HexFilled
	switch st {
	case Awaiting:
		style = StyleAwaiting
	case Idle:
		style, glyph = StyleIdle, HexHollow
	case Done:
		style, glyph = StyleDone, HexHollow
	case Failed:
		style = StyleFailed
	}
	if name != "" {
		glyph += " " + name
	}
	return style.Render(glyph)
}
