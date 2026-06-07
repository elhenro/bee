package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
)

// renderWindowTabs shows the time-window selector with the active one boxed.
func renderWindowTabs(active int) string {
	on := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	off := lipgloss.NewStyle().Foreground(fgOyster)
	parts := make([]string, 0, len(winLabels))
	for i, lbl := range winLabels {
		if i == active {
			parts = append(parts, on.Render("[ "+lbl+" ]"))
		} else {
			parts = append(parts, off.Render("  "+lbl+"  "))
		}
	}
	return strings.Join(parts, " ")
}

// renderUsageHeadline draws the summary line: total tokens, cost, calls, window.
func renderUsageHeadline(s cost.Summary, label string) string {
	tok := lipgloss.NewStyle().Foreground(accentHoney).Bold(true).
		Render(humanTokens(s.Input+s.Output) + " tokens")
	rest := lipgloss.NewStyle().Foreground(fgSmoke).
		Render(fmt.Sprintf("  ·  %s  ·  %d calls", fmtUSD(s.USD), s.Calls))
	win := lipgloss.NewStyle().Foreground(fgOyster).Render("  (" + label + ")")
	return tok + rest + win
}

// renderUsageChart draws a braille usage-over-time chart for the window. Falls
// back to block chars when there are too few buckets for a dense braille line.
func renderUsageChart(series []float64, inner int) string {
	if len(series) == 0 {
		return StyleLabel.Render("  (no series)")
	}
	var bars string
	if len(series) < 4 {
		bars = sparkFromValues(series)
	} else {
		bars = BrailleSparkline(series, inner/2)
	}
	chart := lipgloss.NewStyle().Foreground(accentBee).Render(bars)
	return chart + "  " + StyleLabel.Render("tokens / bucket")
}

// renderProviderBars draws one horizontal bar per provider, sorted by tokens.
func renderProviderBars(w cost.UsageWindow, inner int) string {
	keys := cost.SortedKeys(w.ByProvider)
	sort.SliceStable(keys, func(i, j int) bool {
		return tokensOf(w.ByProvider[keys[i]]) > tokensOf(w.ByProvider[keys[j]])
	})
	total := w.Total.Input + w.Total.Output
	nameW := 10
	barW := inner - nameW - 30
	showBar := barW >= 4
	txt := lipgloss.NewStyle().Foreground(fgAsh)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(fgSmoke).Bold(true).Render("by provider"))
	b.WriteString("\n")
	for _, k := range keys {
		s := w.ByProvider[k]
		tk := tokensOf(s)
		frac := 0.0
		if total > 0 {
			frac = float64(tk) / float64(total)
		}
		line := txt.Render(fmt.Sprintf("  %-*s ", nameW, truncateRune(k, nameW)))
		if showBar {
			line += usageBar(frac, barW)
		}
		line += txt.Render(fmt.Sprintf(" %7s  %8s  %3.0f%%", humanTokens(tk), usageCost(k, s.USD), frac*100))
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// renderModelRows lists per-model totals. Local/unpriced models show "—"
// instead of $0 so the dollar column stays meaningful.
func renderModelRows(m map[string]cost.Summary, inner int) string {
	if len(m) == 0 {
		return StyleLabel.Render("  (none)")
	}
	keys := cost.SortedKeys(m)
	sort.SliceStable(keys, func(i, j int) bool { return m[keys[i]].USD > m[keys[j]].USD })
	nameW := 22
	if inner > 0 && inner-32 > nameW {
		nameW = inner - 32
	}
	txt := lipgloss.NewStyle().Foreground(fgAsh)

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(fgSmoke).Bold(true).Render("by model"))
	b.WriteString("\n")
	for _, k := range keys {
		s := m[k]
		c := "—"
		if s.USD > 0 {
			c = fmtUSD(s.USD)
		}
		row := fmt.Sprintf("  %-*s  %8s  in %7s  out %7s",
			nameW, truncateRune(k, nameW), c, humanTokens(s.Input), humanTokens(s.Output))
		b.WriteString(txt.Render(row))
		b.WriteString("\n")
	}
	return b.String()
}

// renderUsageFallback is shown before any per-call history exists: lifetime
// token totals plus the live session breakdown, so the pane is never blank.
func renderUsageFallback(t *cost.Tracker, inner int) string {
	var b strings.Builder
	if li, lo := cost.LifetimeTotals(); li+lo > 0 {
		head := lipgloss.NewStyle().Foreground(accentHoney).Bold(true).
			Render(humanTokens(int(li+lo)) + " tokens")
		sub := lipgloss.NewStyle().Foreground(fgSmoke).
			Render(fmt.Sprintf("  ·  in %s  out %s  (lifetime)", humanTokens(int(li)), humanTokens(int(lo))))
		b.WriteString(head + sub)
		b.WriteString("\n")
	}
	b.WriteString(StyleLabel.Render("  no per-call history yet — recording starts now"))
	b.WriteString("\n")
	if t != nil && t.Total().Calls > 0 {
		b.WriteString("\n")
		b.WriteString(renderModelRows(t.ByModel(), inner))
	}
	return b.String()
}

// usageBar renders a width-w horizontal bar filled to frac.
func usageBar(frac float64, w int) string {
	if w < 1 {
		w = 1
	}
	f := int(frac*float64(w) + 0.5)
	if f > w {
		f = w
	}
	if frac > 0 && f == 0 {
		f = 1
	}
	full := lipgloss.NewStyle().Foreground(accentHoney).Render(strings.Repeat("█", f))
	empty := lipgloss.NewStyle().Foreground(fgOyster).Render(strings.Repeat("░", w-f))
	return full + empty
}

// usageCost applies the cost-display rules: local providers show "local",
// non-local-but-unpriced show "—", priced show the dollar figure.
func usageCost(provider string, usd float64) string {
	if config.IsLocalProvider(provider) {
		return "local"
	}
	if usd <= 0 {
		return "—"
	}
	return fmtUSD(usd)
}

func tokensOf(s cost.Summary) int { return s.Input + s.Output }

func usageFooter() string {
	return StyleLabel.Render("tab/←→ window · 1-4 jump · r reload · esc close")
}
