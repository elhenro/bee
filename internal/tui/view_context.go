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

// quiet until it matters: faint, then honey, then yellow, then red
func contextTier(pct float64, known bool) (lipgloss.TerminalColor, bool) {
	switch {
	case !known, pct < 0.50:
		return fgSquid, false
	case pct < 0.80:
		return accentHoney, false
	case pct < 0.95:
		return accentBusy, true
	default:
		return semError, true
	}
}

// bee mark + `ctx 12%`, tinted by fill; "?" when the model isn't in the
// windows table (else 0% would look like an empty context)
func (m Model) renderContextHex() string {
	if !m.showBee && !m.showContextPct {
		return ""
	}
	pct, known := m.contextPct()
	// nothing recorded yet: bee mark only, so a fresh start doesn't read
	// as an unknown-window bug
	if !known && !m.hasInputTokens() {
		if m.showBee {
			return m.styles.Dim.Render(glyphBee)
		}
		return ""
	}
	fg, bold := contextTier(pct, known)
	style := lipgloss.NewStyle().Foreground(fg).Bold(bold)
	var out string
	if m.showBee {
		out = style.Render(glyphBee)
	}
	if m.showContextPct {
		label := "?"
		if known {
			label = fmt.Sprintf("%d%%", min(999, int(pct*100+0.5)))
		}
		chip := m.styles.Dim.Render("ctx") + " " + style.Render(label)
		if out != "" {
			out += " " + chip
		} else {
			out = chip
		}
	}
	return out
}

// the bottom hairline as a gauge: `━` fills from the left in the chip's
// color tiers as the context window fills
func (m Model) renderContextBar() string {
	if m.width <= 0 {
		return ""
	}
	w := m.ruleWidth()
	pct, known := m.contextPct()
	if !known && !m.hasInputTokens() {
		return m.renderRule()
	}
	if !known {
		// used but cap unknown: a lone "?" rather than a falsely-empty bar
		return " " + m.styles.Dim.Render("?") + m.styles.Rule.Render(strings.Repeat(glyphRule, w-1))
	}
	fill := min(w, max(0, int(min(pct, 1)*float64(w)+0.5)))
	fg, bold := contextTier(pct, known)
	filled := lipgloss.NewStyle().Foreground(fg).Bold(bold).Render(strings.Repeat(glyphGauge, fill))
	return " " + filled + m.styles.Rule.Render(strings.Repeat(glyphRule, w-fill))
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
