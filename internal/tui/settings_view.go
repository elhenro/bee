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
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render("⬢ Settings")

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
		marker := "  "
		nameStyle := lipgloss.NewStyle().Foreground(fgOyster)
		if i == p.cursor {
			marker = selectMark(true)
			nameStyle = nameStyle.Foreground(accentHoney).Bold(true)
		}
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
