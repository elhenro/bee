package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/cost"
)

// CostPane is the Ctrl+Y modal: session total, recent-turn sparkline, and
// a filterable breakdown by model / provider.
type CostPane struct {
	tracker *cost.Tracker
	open    bool

	// filter dimension cycled with Tab: all → provider → model → all
	filterMode int
	// when filterMode != 0, filterIdx points into the sorted key list for
	// that dimension. j/k cycle through it.
	filterIdx int
}

// NewCostPane returns a closed pane wired to the given tracker. Tracker may
// be nil — then the pane renders an empty state.
func NewCostPane(t *cost.Tracker) *CostPane { return &CostPane{tracker: t} }

// Open reports modal visibility.
func (c *CostPane) Open() bool { return c != nil && c.open }

// ToggleCostPaneMsg flips the pane visibility.
type ToggleCostPaneMsg struct{}

// Update reacts to keys while open. Tab cycles filter dimension; j/k
// cycle the active filter value; esc closes.
func (c *CostPane) Update(msg tea.Msg) (*CostPane, tea.Cmd) {
	if c == nil {
		return c, nil
	}
	switch m := msg.(type) {
	case ToggleCostPaneMsg:
		c.open = !c.open
		return c, nil
	case tea.KeyMsg:
		if !c.open {
			return c, nil
		}
		switch m.String() {
		case "esc", "q":
			c.open = false
		case "tab":
			c.filterMode = (c.filterMode + 1) % 3
			c.filterIdx = 0
		case "j", "down":
			c.moveFilter(1)
		case "k", "up":
			c.moveFilter(-1)
		case "0":
			c.filterMode = 0
			c.filterIdx = 0
		}
	}
	return c, nil
}

// moveFilter advances the active filter index, wrapping at the end.
func (c *CostPane) moveFilter(delta int) {
	keys := c.filterKeys()
	if len(keys) == 0 {
		return
	}
	c.filterIdx = (c.filterIdx + delta + len(keys)) % len(keys)
}

// filterKeys lists the sorted values available for the active dimension.
func (c *CostPane) filterKeys() []string {
	if c.tracker == nil {
		return nil
	}
	switch c.filterMode {
	case 1:
		return cost.SortedKeys(c.tracker.ByProvider())
	case 2:
		return cost.SortedKeys(c.tracker.ByModel())
	default:
		return nil
	}
}

// activeFilter returns the currently-selected (provider, model) pair to
// pass into tracker.Filter. Empty strings mean "any".
func (c *CostPane) activeFilter() (provider, model string) {
	keys := c.filterKeys()
	if len(keys) == 0 || c.filterIdx >= len(keys) {
		return "", ""
	}
	switch c.filterMode {
	case 1:
		return keys[c.filterIdx], ""
	case 2:
		return "", keys[c.filterIdx]
	}
	return "", ""
}

// View renders the modal. Empty tracker → friendly stub; otherwise summary
// + sparkline + breakdown tables.
func (c *CostPane) View(width, height int) string {
	if c == nil || !c.open {
		return ""
	}
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render("⬢ Cost monitor")

	if c.tracker == nil {
		return boxModal(title+"\n\n"+StyleLabel.Render("(no tracker wired)"), width, height)
	}

	prov, model := c.activeFilter()
	filtered := c.tracker.Filter(prov, model)
	tot := summarize(filtered)

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(c.renderFilterChips())
	b.WriteString("\n\n")
	b.WriteString(c.renderTotals(tot))
	b.WriteString("\n")
	b.WriteString(c.renderSparkline(filtered, width-6))
	b.WriteString("\n\n")
	b.WriteString(c.renderBreakdown(width - 6))
	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("tab cycle filter · j/k pick · 0 clear · esc close"))
	return boxModal(b.String(), width, height)
}

// renderFilterChips shows which dimension is active and the selected value.
func (c *CostPane) renderFilterChips() string {
	chipOn := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	chipOff := lipgloss.NewStyle().Foreground(fgOyster)
	chip := func(label string, on bool) string {
		if on {
			return chipOn.Render("[" + label + "]")
		}
		return chipOff.Render(" " + label + " ")
	}
	out := chip("all", c.filterMode == 0) + " " + chip("provider", c.filterMode == 1) + " " + chip("model", c.filterMode == 2)
	prov, model := c.activeFilter()
	if prov != "" || model != "" {
		v := prov + model
		out += "  " + lipgloss.NewStyle().Foreground(accentBee).Render("→ "+v)
	}
	return out
}

// renderTotals draws the headline summary line.
func (c *CostPane) renderTotals(s cost.Summary) string {
	usd := lipgloss.NewStyle().Foreground(accentHoney).Bold(true).Render(fmtUSD(s.USD))
	rest := lipgloss.NewStyle().Foreground(fgSmoke).Render(
		fmt.Sprintf("  %d calls  ·  in %s  ·  out %s", s.Calls, humanTokens(s.Input), humanTokens(s.Output)),
	)
	return usd + rest
}

// renderSparkline draws a unicode bar chart of recent events (or buckets
// the full history when there are too many). Width caps the bar count.
func (c *CostPane) renderSparkline(events []cost.Event, width int) string {
	if width < 8 {
		width = 8
	}
	bars := sparkBars(events, width)
	if bars == "" {
		return StyleLabel.Render("(no data yet)")
	}
	label := StyleLabel.Render(fmt.Sprintf("recent %d turns  $/turn:", min(width, len(events))))
	return label + "\n" + lipgloss.NewStyle().Foreground(accentBee).Render(bars)
}

// renderBreakdown prints two tables: by provider, then by model.
func (c *CostPane) renderBreakdown(width int) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(fgSmoke).Bold(true).Render("by provider"))
	b.WriteString("\n")
	b.WriteString(renderTable(c.tracker.ByProvider(), width))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(fgSmoke).Bold(true).Render("by model"))
	b.WriteString("\n")
	b.WriteString(renderTable(c.tracker.ByModel(), width))
	return b.String()
}

// renderTable formats a summary map as a 4-column aligned listing.
func renderTable(m map[string]cost.Summary, width int) string {
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
		name := truncateRune(k, nameW)
		row := fmt.Sprintf("  %-*s  %8s  in %7s  out %7s",
			nameW, name, fmtUSD(s.USD), humanTokens(s.Input), humanTokens(s.Output))
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
			end := i + chunk
			if end > len(events) {
				end = len(events)
			}
			var sum float64
			for _, e := range events[i:end] {
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
		if v > maxV {
			maxV = v
		}
	}
	if maxV <= 0 {
		// all-zero: render the lowest block so the bar still appears.
		return strings.Repeat(string(blocks[0]), len(vals))
	}
	var b strings.Builder
	for _, v := range vals {
		idx := int(v / maxV * float64(len(blocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		b.WriteRune(blocks[idx])
	}
	return b.String()
}

// fmtUSD mirrors view.go's formatUSD; duplicated to keep the pane self-contained.
func fmtUSD(usd float64) string {
	if usd < 1 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
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

// truncateRune cuts a string to maxRune runes, appending …. ASCII-fast-path.
func truncateRune(s string, maxRune int) string {
	if maxRune <= 0 || len(s) <= maxRune {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRune {
		return s
	}
	return string(r[:maxRune-1]) + "…"
}

