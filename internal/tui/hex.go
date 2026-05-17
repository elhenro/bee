// Package tui implements the bubbletea-driven interactive UI for bee.
//
// Visual primitives live here: hex glyphs and the honey palette. Slice 3A
// composes Hive + Workspace via NewHive / NewWorkspace factories.
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Hex glyphs (PLAN §8b.1) and honey palette.
const (
	HexFilled      = "⬢"
	HexHollow      = "⬢"
	ColorHoneyGold = "#F2B233"
	ColorAmber     = "#E08A1E"
	ColorDim       = "#5F5F5F"
	ColorAccent    = "#FFE066"
	ColorDanger    = "#D9534F"
	ColorAddFg     = "#7FBF6B"
	ColorDelFg     = "#D9534F"
	ColorMuted     = "#888888"
)

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

// Pre-built styles. Public so 3A may compose without redefining.
var (
	StyleActive   = fg(ColorHoneyGold).Bold(true)
	StyleAwaiting = fg(ColorAccent).Bold(true)
	StyleIdle     = fg(ColorAmber)
	StyleDone     = fg(ColorDim)
	StyleFailed   = fg(ColorDanger).Bold(true)
	StyleLabel    = fg(ColorMuted)
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

type cellLook struct {
	glyph string
	style lipgloss.Style
}

var cellLooks = map[BeeState]cellLook{
	Active: {HexFilled, StyleActive}, Awaiting: {HexFilled, StyleAwaiting},
	Idle: {HexHollow, StyleIdle}, Done: {HexHollow, StyleDone},
	Failed: {HexFilled, StyleFailed},
}

func renderCell(st BeeState, name string) string {
	look := cellLooks[st]
	if look.glyph == "" {
		look = cellLooks[Active]
	}
	if name == "" {
		return look.style.Render(look.glyph)
	}
	return look.style.Render(look.glyph + " " + name)
}
