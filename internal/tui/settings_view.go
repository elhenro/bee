package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the modal.
func (p *SettingsPane) View(width, height int) string {
	if p == nil || !p.open {
		return ""
	}
	title := paneTitle("⬢ Settings")

	hl := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(p.filter.View())
	b.WriteString("\n\n")

	if len(p.matches) == 0 {
		b.WriteString(StyleLabel.Render("  no matches"))
		b.WriteString("\n")
	}

	// title, filter, blank, blank, hint, border (2), "+N more"
	rng := windowRange(len(p.matches), p.cursor, paneRows(height, 8))
	for i := rng.start; i < rng.end; i++ {
		m := p.matches[i]
		r := settingsRows[m.Index]
		marker, nameStyle := paneRow(i == p.cursor)
		var toggle string
		if r.isInt {
			toggle = fmt.Sprintf("[%d]", p.outerPad)
		} else {
			toggle = "[ ]"
			if p.on[r.key] {
				toggle = lipgloss.NewStyle().Foreground(accentHoney).Render("[x]")
			}
		}
		b.WriteString(marker)
		b.WriteString(toggle)
		b.WriteString("  ")
		b.WriteString(HighlightLabel(r.label, m.MatchedIndexes, nameStyle, hl, 22))
		b.WriteString("  ")
		b.WriteString(StyleLabel.Render(r.desc))
		b.WriteString("\n")
	}

	if more := moreRow(rng, len(p.matches)); more != "" {
		b.WriteString(more + "\n")
	}
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("type filter · ↑/↓ pick · enter/tab toggle · esc close · saved to ~/.bee/config.toml"))
	return boxRows(b.String(), width)
}
