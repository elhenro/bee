package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// maxPaletteRows caps visible rows so the picker never blots out scrollback
// on short terminals. Overflow is summarized with a "+N more" footer.
const maxPaletteRows = 8

// View renders the palette as a borderless, dense strip designed to sit
// directly above the input bar. Width is taken from SetWidth — falls back
// to a sensible default when unset (e.g. tests).
func (p PaletteModel) View() string {
	if !p.Active {
		return ""
	}
	w := p.width
	if w <= 0 {
		w = 80
	}

	dim := lipgloss.NewStyle().Foreground(fgOyster)
	hl := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	cmdGlyph := lipgloss.NewStyle().Foreground(accentYou)
	skillGlyph := lipgloss.NewStyle().Foreground(semSuccess)
	selMark := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	nameSel := lipgloss.NewStyle().Foreground(fgButter).Bold(true)
	nameNorm := lipgloss.NewStyle().Foreground(fgSmoke)

	if len(p.matches) == 0 {
		return dim.Render("  no matches")
	}

	total := len(p.matches)
	start := 0
	if total > maxPaletteRows {
		if p.selected >= maxPaletteRows {
			start = p.selected - maxPaletteRows + 1
		}
		if start+maxPaletteRows > total {
			start = total - maxPaletteRows
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + maxPaletteRows
	if end > total {
		end = total
	}
	rows := p.matches[start:end]
	overflowAbove := start
	overflowBelow := total - end

	var b strings.Builder
	if overflowAbove > 0 {
		b.WriteString(dim.Render("  ↑ " + strconv.Itoa(overflowAbove) + " more"))
		b.WriteString("\n")
	}
	for i, m := range rows {
		entry := p.pool[m.Index]
		absIdx := start + i

		var line strings.Builder
		if absIdx == p.selected {
			line.WriteString(selMark.Render("›"))
		} else {
			line.WriteString(" ")
		}
		line.WriteString(" ")

		glyph := "/"
		gs := cmdGlyph
		if entry.Kind == EntrySkill {
			glyph = "#"
			gs = skillGlyph
		}
		line.WriteString(gs.Render(glyph))

		// highlight matched runes within name range only. fuzzy returns
		// indices into "name description"; mask to [0, nameLen).
		nameLen := len(entry.Name)
		matchSet := map[int]struct{}{}
		for _, idx := range m.MatchedIndexes {
			if idx >= 0 && idx < nameLen {
				matchSet[idx] = struct{}{}
			}
		}
		ns := nameNorm
		if absIdx == p.selected {
			ns = nameSel
		}
		for j := 0; j < nameLen; j++ {
			ch := string(entry.Name[j])
			if _, ok := matchSet[j]; ok {
				line.WriteString(hl.Render(ch))
			} else {
				line.WriteString(ns.Render(ch))
			}
		}

		if entry.Description != "" {
			line.WriteString(dim.Render("  " + entry.Description))
		}

		row := line.String()
		if lipglossWidth(row) > w {
			row = truncateVisible(row, w)
		}
		b.WriteString(row)
		if i < len(rows)-1 || overflowBelow > 0 {
			b.WriteString("\n")
		}
	}
	if overflowBelow > 0 {
		b.WriteString(dim.Render("  ↓ " + strconv.Itoa(overflowBelow) + " more"))
	}
	return b.String()
}
