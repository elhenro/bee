package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// View renders the picker as a borderless dense strip — same aesthetic as
// the slash-command palette. fzf-style filter input, matched-rune highlights,
// two-stage flow (provider → model).
func (p *Picker) View() string {
	if !p.active {
		return ""
	}
	w := p.width
	if w <= 0 {
		w = 80
	}

	dim := lipgloss.NewStyle().Foreground(fgOyster)
	headDim := lipgloss.NewStyle().Foreground(fgSquid)
	headBold := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)

	var crumb string
	if p.focus == colProviders {
		crumb = headBold.Render("provider") + headDim.Render(" › model")
	} else {
		crumb = headDim.Render("provider › ") + headBold.Render(p.currentProvider)
	}

	body := p.renderActive(w)
	hint := "enter pick · esc back · ↑↓ nav"
	if p.focus == colModels {
		hint += " · ^r refresh"
	}
	return strings.Join([]string{crumb, p.filter.View(), body, dim.Render(hint)}, "\n")
}

// renderActive renders whichever stage is focused.
func (p *Picker) renderActive(w int) string {
	if p.focus == colProviders {
		matches := p.matchProviders()
		return renderRows(matches, p.provSel, p.provQuery, "/", w, func(idx int) (string, string) {
			pi := p.providers[idx]
			return pi.name, pi.cfg.BaseURL
		})
	}
	if p.currentProvider != "" && p.loading[p.currentProvider] {
		return "  " + p.spin.View() + " " + lipgloss.NewStyle().Foreground(fgOyster).Render("loading "+p.currentProvider+"/models…")
	}
	if err := p.loadErr[p.currentProvider]; err != nil {
		errStyle := lipgloss.NewStyle().Foreground(semError)
		dim := lipgloss.NewStyle().Foreground(fgOyster)
		msg := compactError(err.Error(), w-len("  ✗ error: ")-1)
		hint := "ctrl+r to retry · esc back"
		if isAuthErr(err.Error()) {
			hint = "ctrl+l enroll api key (/login " + p.currentProvider + ") · ctrl+r retry · esc back"
		}
		return "  " + errStyle.Render("✗ error: "+msg) + "\n  " + dim.Render(hint)
	}
	matches := p.matchModels()
	models := p.modelsByProvider[p.currentProvider]
	return renderRows(matches, p.modelSel, p.modelQuery, "#", w, func(idx int) (string, string) {
		mi := models[idx]
		return mi.display, mi.desc
	})
}

// renderRows draws palette-style rows with ›/glyph/name/desc, matched-rune
// highlights, and ↑/↓ overflow indicators.
func renderRows(matches []fuzzy.Match, sel int, _ string, glyph string, w int, rowFn func(int) (string, string)) string {
	dim := lipgloss.NewStyle().Foreground(fgOyster)
	hl := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	selMark := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	nameSel := lipgloss.NewStyle().Foreground(fgButter).Bold(true)
	nameNorm := lipgloss.NewStyle().Foreground(fgSmoke)
	gs := lipgloss.NewStyle().Foreground(accentBee)

	if len(matches) == 0 {
		return dim.Render("  no matches")
	}

	total := len(matches)
	start := 0
	if total > maxPickerRows {
		if sel >= maxPickerRows {
			start = sel - maxPickerRows + 1
		}
		if start+maxPickerRows > total {
			start = total - maxPickerRows
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + maxPickerRows
	if end > total {
		end = total
	}

	var b strings.Builder
	if start > 0 {
		b.WriteString(dim.Render(fmt.Sprintf("  ↑ %d more", start)))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		m := matches[i]
		name, desc := rowFn(m.Index)

		var line strings.Builder
		if i == sel {
			line.WriteString(selMark.Render("›"))
		} else {
			line.WriteString(" ")
		}
		line.WriteString(" ")
		line.WriteString(gs.Render(glyph))
		line.WriteString(" ")

		// highlight matched runes within the name only; fuzzy gives indices
		// into the full haystack "name + space + extras", mask to name range
		nameLen := len(name)
		hits := map[int]struct{}{}
		for _, idx := range m.MatchedIndexes {
			if idx >= 0 && idx < nameLen {
				hits[idx] = struct{}{}
			}
		}
		ns := nameNorm
		if i == sel {
			ns = nameSel
		}
		for j := 0; j < nameLen; j++ {
			ch := string(name[j])
			if _, ok := hits[j]; ok {
				line.WriteString(hl.Render(ch))
			} else {
				line.WriteString(ns.Render(ch))
			}
		}
		if desc != "" {
			line.WriteString(dim.Render("  " + desc))
		}
		row := line.String()
		if lipglossWidth(row) > w {
			row = truncateVisible(row, w)
		}
		b.WriteString(row)
		if i < end-1 || end < total {
			b.WriteString("\n")
		}
	}
	if end < total {
		b.WriteString(dim.Render(fmt.Sprintf("  ↓ %d more", total-end)))
	}
	return b.String()
}

// compactError flattens multi-line/JSON error bodies into one short line so
// the picker doesn't blow up vertically with stray }, ], " fragments from
// upstream API responses.
func compactError(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	if maxLen < 16 {
		maxLen = 16
	}
	if len(s) > maxLen {
		s = s[:maxLen-1] + "…"
	}
	return s
}
