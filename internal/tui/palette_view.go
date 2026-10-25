package tui

import "github.com/charmbracelet/lipgloss"

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

	cmdGlyph := lipgloss.NewStyle().Foreground(fgSquid)
	skillGlyph := lipgloss.NewStyle().Foreground(accentCode)
	return renderPickRows(p.matches, p.selected, w, maxPaletteRows, "", func(idx int) pickRow {
		e := p.pool[idx]
		if e.Kind == EntrySkill {
			return pickRow{glyph: "#", glyphStyle: skillGlyph, name: e.Name, desc: e.Description}
		}
		return pickRow{glyph: "/", glyphStyle: cmdGlyph, name: e.Name, desc: e.Description}
	})
}
