package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/cost"
)

// renderTable formats a summary map as a 4-column aligned listing. The row
// whose key equals selected gets the selection marker.
func renderTable(m map[string]cost.Summary, selected string, width int) string {
	if len(m) == 0 {
		return StyleLabel.Render("  (none)")
	}
	keys := cost.SortedKeys(m)
	// sort by USD desc for top-of-list relevance
	sort.SliceStable(keys, func(i, j int) bool { return m[keys[i]].USD > m[keys[j]].USD })

	nameW := 24
	if width > 0 && width-40 > nameW {
		nameW = width - 40
	}
	var b strings.Builder
	for _, k := range keys {
		s := m[k]
		name := truncateRunes(k, nameW)
		row := fmt.Sprintf("%-*s  %8s  in %7s  out %7s",
			nameW, name, formatUSD(s.USD), humanTokens(s.Input), humanTokens(s.Output))
		b.WriteString(selectMark(selected != "" && k == selected))
		b.WriteString(lipgloss.NewStyle().Foreground(fgAsh).Render(row))
		b.WriteString("\n")
	}
	return b.String()
}

// summarize rolls up a filtered event slice. Kept local because cost.Summary
// is a value type and tracker doesn't expose a bulk-sum helper.
func summarize(events []cost.Event) cost.Summary {
	var s cost.Summary
	for _, e := range events {
		s.Calls++
		s.Input += e.Input
		s.Output += e.Output
		s.USD += e.USD
	}
	return s
}

// sparkBars renders the dollar amount of each event as a unicode bar.
// When events outnumber width slots, buckets the history evenly so the
// resulting bar count fits.
func sparkBars(events []cost.Event, width int) string {
	if len(events) == 0 || width <= 0 {
		return ""
	}
	values := make([]float64, 0, len(events))
	if len(events) <= width {
		for _, e := range events {
			values = append(values, e.USD)
		}
	} else {
		// bucket: chunk size = ceil(len/width)
		chunk := (len(events) + width - 1) / width
		for i := 0; i < len(events); i += chunk {
			var sum float64
			for _, e := range events[i:min(i+chunk, len(events))] {
				sum += e.USD
			}
			values = append(values, sum)
		}
	}
	return sparkFromValues(values)
}

// sparkFromValues normalizes values to [0, 7] and maps each to a block char.
func sparkFromValues(vals []float64) string {
	blocks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	maxV := 0.0
	for _, v := range vals {
		maxV = max(maxV, v)
	}
	if maxV <= 0 {
		// all-zero: render the lowest block so the bar still appears.
		return strings.Repeat(string(blocks[0]), len(vals))
	}
	var b strings.Builder
	for _, v := range vals {
		b.WriteRune(blocks[min(max(int(v/maxV*float64(len(blocks)-1)), 0), len(blocks)-1)])
	}
	return b.String()
}

// humanTokens condenses big token counts: 1.2K, 3.4M.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
