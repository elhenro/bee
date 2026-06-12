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
// by the most recent turn's input, plus a `known` flag. known is false when
// no costs tracked, no events yet, or the model's window is unknown. Callers
// surface the unknown case as a dim "?" so a missing ContextWindow entry
// (e.g. a brand-new openrouter listing the API doesn't advertise) doesn't
// silently render as 0% and pretend everything is fine.
func (m Model) contextPct() (pct float64, known bool) {
	if m.costs == nil {
		return 0, false
	}
	in := m.costs.LastInput()
	if in <= 0 {
		return 0, false
	}
	cap := llm.ContextWindow(m.model)
	if cap <= 0 {
		// known=false: the model id isn't in the hardcoded table and the
		// provider's /v1/models didn't surface a context_length. Callers
		// detect "user has used context but we don't know how much" by
		// checking costs.LastInput() > 0 alongside known==false.
		return 0, false
	}
	return float64(in) / float64(cap), true
}

// hasInputTokens reports whether the most recent turn has any input tokens
// recorded. Lets the context hex/bar distinguish "unknown because fresh"
// (silent) from "unknown because model isn't in the table" (show "?").
func (m Model) hasInputTokens() bool {
	return m.costs != nil && m.costs.LastInput() > 0
}

// renderContextHex draws the pie-style fill indicator. A 🐝 emoji with
// colour tier escalates with fill so a glance tells
// you "fresh" vs "almost full". Percent label appears once anything's used,
// or a "?" when the model id isn't in the known-windows table (otherwise
// 0% would be indistinguishable from a genuinely-empty context).
func (m Model) renderContextHex() string {
	if !m.showBee && !m.showContextPct {
		return ""
	}
	pct, known := m.contextPct()
	// not-yet-meaningful (no events recorded): show the bee chip only.
	// Distinguishing this from "known=false but user has used context"
	// prevents a 0%-on-fresh-start from looking like an unknown-window bug.
	if !known && !m.hasInputTokens() {
		if m.showBee {
			return lipgloss.NewStyle().Foreground(fgSquid).Render("🐝")
		}
		return ""
	}
	var fg lipgloss.TerminalColor
	bold := false
	switch {
	case !known, pct < 0.01:
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
	if m.showContextPct {
		var label string
		if !known {
			// dim "?" — same width as "0%" so the chip doesn't jitter
			// when the cap is finally learned mid-session.
			label = style.Render("?")
		} else {
			p := int(pct*100 + 0.5)
			if p > 999 {
				p = 999
			}
			label = style.Render(fmt.Sprintf("%d%%", p))
		}
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
	pct, known := m.contextPct()
	if !known && !m.hasInputTokens() {
		// fresh state: nothing to fill, but render the rule so the bottom
		// edge still has the elegant chrome.
		return lipgloss.NewStyle().Foreground(fgOyster).Render(strings.Repeat("─", m.width))
	}
	if !known {
		// cap unknown but the user has used context: dim the strip, drop a
		// single "?" at the left edge so the bar reads as "I have data
		// but no cap" rather than a falsely-empty 0%.
		dim := lipgloss.NewStyle().Foreground(fgOyster).Render(strings.Repeat("─", m.width-1))
		glyph := lipgloss.NewStyle().Foreground(fgSquid).Render("?")
		return glyph + dim
	}
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
