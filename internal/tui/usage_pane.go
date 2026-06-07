package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/cost"
)

// time windows cycled in the usage pane. Rolling, not calendar: "last N".
const (
	winToday = iota
	win7d
	win30d
	winAll
)

var winLabels = []string{"Today", "7d", "30d", "All"}

// UsagePane is the /usage modal: historical token + cost usage read from the
// persisted usage log, broken down by time window, provider, and model. Unlike
// CostPane (live, per-session) it snapshots cross-session history on open.
type UsagePane struct {
	tracker *cost.Tracker // live session, for the empty-history fallback
	open    bool
	window  int
	win     [4]cost.UsageWindow // day, week, month, all — snapshot at open
	loaded  bool
}

// NewUsagePane returns a closed pane. The tracker feeds the fallback shown
// before any per-call history has been recorded; it may be nil.
func NewUsagePane(t *cost.Tracker) *UsagePane { return &UsagePane{tracker: t} }

// Open reports modal visibility.
func (u *UsagePane) Open() bool { return u != nil && u.open }

// ToggleUsagePaneMsg flips the pane visibility.
type ToggleUsagePaneMsg struct{}

// Update reacts to keys while open. Tab / arrows switch the time window;
// 1-4 jump; r reloads; esc closes.
func (u *UsagePane) Update(msg tea.Msg) (*UsagePane, tea.Cmd) {
	if u == nil {
		return u, nil
	}
	switch m := msg.(type) {
	case ToggleUsagePaneMsg:
		u.open = !u.open
		if u.open {
			u.reload()
		}
		return u, nil
	case tea.KeyMsg:
		if !u.open {
			return u, nil
		}
		switch m.String() {
		case "esc", "q":
			u.open = false
		case "tab", "right", "l":
			u.window = (u.window + 1) % len(winLabels)
		case "shift+tab", "left", "h":
			u.window = (u.window - 1 + len(winLabels)) % len(winLabels)
		case "1":
			u.window = winToday
		case "2":
			u.window = win7d
		case "3":
			u.window = win30d
		case "4":
			u.window = winAll
		case "r":
			u.reload()
		}
	}
	return u, nil
}

// reload snapshots the four windows from the persisted log.
func (u *UsagePane) reload() {
	d, w, mo, a, _ := cost.UsageOverview()
	u.win = [4]cost.UsageWindow{d, w, mo, a}
	u.loaded = true
}

// View renders the modal: title, window tabs, then either the empty-history
// fallback or the selected window's headline, chart, and breakdowns.
func (u *UsagePane) View(width, height int) string {
	if u == nil || !u.open {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(accentHoney).Bold(true).Render("⬢ Usage overview")
	inner := width - 6

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(renderWindowTabs(u.window))
	b.WriteString("\n\n")

	if !u.loaded || u.win[winAll].Total.Calls == 0 {
		b.WriteString(renderUsageFallback(u.tracker, inner))
		b.WriteString("\n")
		b.WriteString(StyleLabel.Render("esc close"))
		return boxModal(b.String(), width, height)
	}

	w := u.win[u.window]
	if w.Total.Calls == 0 {
		b.WriteString(StyleLabel.Render("  (no usage in this window)"))
		b.WriteString("\n\n")
		b.WriteString(usageFooter())
		return boxModal(b.String(), width, height)
	}

	b.WriteString(renderUsageHeadline(w.Total, winLabels[u.window]))
	b.WriteString("\n")
	if inner >= 16 {
		b.WriteString(renderUsageChart(w.Series, inner))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(renderProviderBars(w, inner))
	b.WriteString("\n")
	b.WriteString(renderModelRows(w.ByModel, inner))
	if w.Estimated {
		b.WriteString(lipgloss.NewStyle().Foreground(fgOyster).Italic(true).Render("  cost estimated from price table"))
		b.WriteString("\n")
	}
	b.WriteString(usageFooter())
	return boxModal(b.String(), width, height)
}
