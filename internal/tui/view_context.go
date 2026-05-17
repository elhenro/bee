package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/llm"
)

// liveBudget returns the row budget available for the streaming live region,
// computed as terminal height minus every other non-mid part (chrome) and
// the inter-part newline separators. Returns 0 when height is unknown or
// chrome already fills the screen — caller treats 0 as "no clipping".
func liveBudget(termH int, parts ...string) int {
	if termH <= 0 {
		return 0
	}
	chrome := 0
	nonEmpty := 0
	for _, p := range parts {
		if p == "" {
			continue
		}
		chrome += lipgloss.Height(p)
		nonEmpty++
	}
	// `parts` joined with "\n"; with mid present there's one extra separator
	// between mid and the rest. Final "\n" between blocks costs 1 row each.
	separators := nonEmpty // mid + nonEmpty parts → nonEmpty separators
	// reserve 1 row for the cursor / inline-render safety margin.
	budget := termH - chrome - separators - 1
	if budget < 1 {
		return 1
	}
	return budget
}

// contextPct returns the fraction of the active model's context window used
// by the most recent turn's input. 0 when no costs tracked, no events yet,
// or the model's window is unknown.
func (m Model) contextPct() float64 {
	if m.costs == nil {
		return 0
	}
	in := m.costs.LastInput()
	if in <= 0 {
		return 0
	}
	cap := llm.ContextWindow(m.model)
	if cap <= 0 {
		return 0
	}
	return float64(in) / float64(cap)
}

// renderContextHex draws the pie-style fill indicator. A 🐝 emoji with
// colour tier escalates with fill so a glance tells
// you "fresh" vs "almost full". Percent label appears once anything's used.
// you "fresh" vs "almost full". Percent label appears once anything's used.
func (m Model) renderContextHex() string {
	if !m.showBee && !m.showContextPct {
		return ""
	}
	pct := m.contextPct()
	var fg lipgloss.TerminalColor
	bold := false
	switch {
	case pct < 0.01:
		fg = fgSquid
	case pct < 0.50:
		fg = accentBee
	case pct < 0.80:
		fg = accentHoney
	case pct < 0.95:
		fg = accentBusy
		bold = true
	default:
		fg = semError
		bold = true
	}
	style := lipgloss.NewStyle().Foreground(fg).Bold(bold)
	var out string
	if m.showBee {
		out = style.Render("🐝")
	}
	if m.showContextPct && pct > 0 {
		// rounded percent; cap display at 999% to avoid layout breaks if
		// LastInput somehow exceeds the window.
		p := int(pct*100 + 0.5)
		if p > 999 {
			p = 999
		}
		label := style.Render(fmt.Sprintf("%d%%", p))
		if out != "" {
			out += " " + label
		} else {
			out = label
		}
	}
	return out
}

// renderContextBar draws a thin full-width progress strip pinned to the
// terminal's bottom edge. Empty state is a quiet ─ rule in oyster; as the
// active turn's input tokens fill the model's context window, the leading
// portion thickens to ━ and steps through the same color tiers as the hex
// glyph (bee → honey → busy → error). Always rendered so the rule reads as
// elegant chrome, not a transient indicator.
func (m Model) renderContextBar() string {
	if m.width <= 0 {
		return ""
	}
	pct := m.contextPct()
	if pct > 1 {
		pct = 1
	}
	fill := int(pct*float64(m.width) + 0.5)
	if fill < 0 {
		fill = 0
	}
	if fill > m.width {
		fill = m.width
	}
	var fg lipgloss.TerminalColor
	bold := false
	switch {
	case pct < 0.01:
		fg = fgOyster
	case pct < 0.50:
		fg = accentBee
	case pct < 0.80:
		fg = accentHoney
	case pct < 0.95:
		fg = accentBusy
		bold = true
	default:
		fg = semError
		bold = true
	}
	filled := lipgloss.NewStyle().Foreground(fg).Bold(bold).Render(strings.Repeat("━", fill))
	rest := lipgloss.NewStyle().Foreground(fgOyster).Render(strings.Repeat("─", m.width-fill))
	return filled + rest
}

// tokensHuman formats a token count compactly: 1234 → "1.2k", 1_500_000 → "1.5M".
// Sub-1000 stays bare. One decimal point until 100, none above.
func tokensHuman(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		v := float64(n) / 1000
		if v < 10 {
			return fmt.Sprintf("%.1fk", v)
		}
		return fmt.Sprintf("%dk", int(v+0.5))
	default:
		v := float64(n) / 1_000_000
		if v < 10 {
			return fmt.Sprintf("%.1fM", v)
		}
		return fmt.Sprintf("%dM", int(v+0.5))
	}
}
