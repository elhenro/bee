package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/cost"
)

// renderUsageText is the compact plain-text overview for headless contexts:
// the four time windows as a headline grid, then the all-time per-provider
// breakdown. No styling so it reads cleanly when echoed into scrollback.
func renderUsageText() string {
	d, w, mo, a, err := cost.UsageOverview()
	if err != nil || a.Total.Calls == 0 {
		li, lo := cost.LifetimeTotals()
		return fmt.Sprintf("usage overview\n  lifetime  %s tok  (in %s / out %s)\n  no per-call history yet",
			humanTokens(int(li+lo)), humanTokens(int(li)), humanTokens(int(lo)))
	}
	var b strings.Builder
	b.WriteString("usage overview\n")
	for _, r := range []struct {
		name string
		w    cost.UsageWindow
	}{{"today", d}, {"7d", w}, {"30d", mo}, {"all", a}} {
		s := r.w.Total
		b.WriteString(fmt.Sprintf("  %-6s %9s tok  %9s  %5d calls\n",
			r.name, humanTokens(s.Input+s.Output), fmtUSD(s.USD), s.Calls))
	}
	b.WriteString("\nby provider (all-time)\n")
	keys := cost.SortedKeys(a.ByProvider)
	sort.SliceStable(keys, func(i, j int) bool {
		return tokensOf(a.ByProvider[keys[i]]) > tokensOf(a.ByProvider[keys[j]])
	})
	for _, k := range keys {
		s := a.ByProvider[k]
		b.WriteString(fmt.Sprintf("  %-12s %8s  %9s\n", truncateRune(k, 12), humanTokens(tokensOf(s)), usageCost(k, s.USD)))
	}
	return b.String()
}
