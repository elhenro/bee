package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/loop"
)

func TestCostPane_EmptyState(t *testing.T) {
	c := NewCostPane(cost.New())
	c, _ = c.Update(ToggleCostPaneMsg{})
	if !c.Open() {
		t.Fatal("pane should open on toggle")
	}
	out := c.View(80, 24)
	if !strings.Contains(out, "Cost monitor") {
		t.Fatalf("missing title: %q", out)
	}
	if !strings.Contains(out, "no data yet") {
		t.Fatalf("missing empty sparkline label: %q", out)
	}
}

func TestCostPane_RendersTotals(t *testing.T) {
	tr := cost.New()
	tr.Record("openai", "gpt-4o-mini", 1_000_000, 500_000)
	c := NewCostPane(tr)
	c, _ = c.Update(ToggleCostPaneMsg{})
	out := c.View(120, 40)
	// total: 1M * 0.15 + 0.5M * 0.60 = 0.15 + 0.30 = $0.45
	if !strings.Contains(out, "$0.4500") {
		t.Fatalf("missing total: %q", out)
	}
	if !strings.Contains(out, "gpt-4o-mini") {
		t.Fatalf("missing model row: %q", out)
	}
	if !strings.Contains(out, "openai") {
		t.Fatalf("missing provider row: %q", out)
	}
}

func TestCostPane_TabCyclesFilter(t *testing.T) {
	tr := cost.New()
	tr.Record("openai", "gpt-4o-mini", 100, 100)
	tr.Record("anthropic", "claude-sonnet-4-5", 100, 100)
	c := NewCostPane(tr)
	c, _ = c.Update(ToggleCostPaneMsg{})

	// initial: all
	if c.filterMode != 0 {
		t.Fatalf("filterMode start: %d", c.filterMode)
	}
	// tab -> provider
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab})
	if c.filterMode != 1 {
		t.Fatalf("after first tab: %d", c.filterMode)
	}
	prov, model := c.activeFilter()
	if prov == "" || model != "" {
		t.Fatalf("expected provider filter, got prov=%q model=%q", prov, model)
	}
	// tab -> model
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab})
	prov, model = c.activeFilter()
	if model == "" || prov != "" {
		t.Fatalf("expected model filter, got prov=%q model=%q", prov, model)
	}
	// tab -> all again
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab})
	if c.filterMode != 0 {
		t.Fatalf("wrap-around: %d", c.filterMode)
	}
}

func TestCostPane_EscClosesPane(t *testing.T) {
	c := NewCostPane(cost.New())
	c, _ = c.Update(ToggleCostPaneMsg{})
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if c.Open() {
		t.Fatal("esc should close")
	}
}

func TestSparkBars_BucketsLongHistory(t *testing.T) {
	events := make([]cost.Event, 100)
	for i := range events {
		events[i] = cost.Event{USD: float64(i)}
	}
	out := sparkBars(events, 10)
	if len([]rune(out)) != 10 {
		t.Fatalf("expected 10 bars, got %d (%q)", len([]rune(out)), out)
	}
}

func TestFormatUSD_Precision(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.0001, "$0.0001"},
		{0.5, "$0.5000"},
		{1.5, "$1.50"},
		{1234.567, "$1234.57"},
	}
	for _, tc := range cases {
		if got := formatUSD(tc.in); got != tc.want {
			t.Errorf("formatUSD(%v) = %q; want %q", tc.in, got, tc.want)
		}
	}
}

func TestTopBar_ShowsCostWhenTracked(t *testing.T) {
	tr := cost.New()
	tr.Record("openai", "gpt-4o-mini", 1_000_000, 500_000)
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(tr)
	m.width = 120
	out := m.renderTopBar()
	if !strings.Contains(out, "$0.4500") {
		t.Fatalf("top bar missing cost: %q", out)
	}
}

// local providers (ollama, lmstudio) report $0 and the cost badge is
// suppressed entirely — even if a tracker happens to carry priced events
// from a prior hosted session.
func TestTopBar_HidesCostOnLocalProvider(t *testing.T) {
	tr := cost.New()
	tr.Record("openai", "gpt-4o-mini", 1_000_000, 500_000)
	eng := &loop.Engine{Cfg: config.Config{DefaultProvider: "ollama"}}
	m := NewModel(eng, "/tmp/work", "llama3.1:8b", "workspace-write", caveman.Default).WithCostTracker(tr)
	m.width = 120
	out := m.renderTopBar()
	if strings.Contains(out, "$") {
		t.Fatalf("local provider should hide cost badge: %q", out)
	}
}

// openCostMsg short-circuits with an error when the active provider is
// local — the pane carries no useful info and would just show $0.
func TestOpenCost_ShortCircuitsOnLocal(t *testing.T) {
	eng := &loop.Engine{Cfg: config.Config{DefaultProvider: "ollama"}}
	m := NewModel(eng, "/tmp/work", "llama3.1:8b", "workspace-write", caveman.Default).WithCostTracker(cost.New())
	updated, cmd := m.Update(openCostMsg{})
	mm := updated.(Model)
	if cmd != nil {
		t.Fatalf("expected nil cmd, got %T", cmd)
	}
	if mm.lastErr == "" {
		t.Fatal("expected lastErr to be set")
	}
	if mm.costPane != nil && mm.costPane.Open() {
		t.Fatal("cost pane should stay closed on local provider")
	}
}

func TestTopBar_HidesCostWhenIdle(t *testing.T) {
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(cost.New())
	m.width = 120
	out := m.renderTopBar()
	if strings.Contains(out, "$") {
		t.Fatalf("top bar should hide cost with zero calls: %q", out)
	}
}

func TestCostFlash_StartsAndRendersDelta(t *testing.T) {
	tr := cost.New()
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(tr)
	m.width = 140
	// Simulate a turn landing: record an event, then trigger the flash.
	tr.Record("openai", "gpt-4o-mini", 1_000_000, 500_000)
	if cmd := m.maybeStartCostFlash(); cmd == nil {
		t.Fatal("expected flash cmd after first event")
	}
	if m.costFlashUntil == 0 {
		t.Fatal("flash should be armed")
	}
	out := m.renderTopBar()
	if !strings.Contains(out, "(+$0.4500)") {
		t.Fatalf("topbar missing delta chip: %q", out)
	}
}

func TestCostFlash_SkipsWhenNoPricedDelta(t *testing.T) {
	tr := cost.New()
	m := NewModel(nil, "/tmp/work", "llama3.1:8b", "workspace-write", caveman.Default).WithCostTracker(tr)
	tr.Record("ollama", "llama3.1:8b", 1000, 500) // unpriced => $0
	if cmd := m.maybeStartCostFlash(); cmd != nil {
		t.Fatal("flash should stay quiet for zero-cost events")
	}
}

func TestTopBar_HexEmptyByDefault(t *testing.T) {
	m := NewModel(nil, "/tmp/work", "claude-sonnet-4-6", "workspace-write", caveman.Default).WithCostTracker(cost.New())
	m.width = 120
	out := stripANSI(m.renderTopBar())
	if !strings.Contains(out, "🐝") {
		t.Fatalf("empty bee glyph missing: %q", out)
	}
	if strings.Contains(out, "%") {
		t.Fatalf("empty bar should hide percent: %q", out)
	}
	if strings.Contains(out, "bee ·") {
		t.Fatalf("topbar should drop the 'bee' label: %q", out)
	}
}

func TestTopBar_HexFillsWithContext(t *testing.T) {
	tr := cost.New()
	// 100k tokens of input on a 200k window => ~50%
	tr.Record("anthropic", "claude-sonnet-4-6", 100_000, 1_000)
	m := NewModel(nil, "/tmp/work", "claude-sonnet-4-6", "workspace-write", caveman.Default).WithCostTracker(tr)
	m.width = 120
	out := stripANSI(m.renderTopBar())
	if !strings.Contains(out, "🐝") {
		t.Fatalf("bee glyph missing: %q", out)
	}
	if !strings.Contains(out, "50%") {
		t.Fatalf("expected 50%% label: %q", out)
	}
}

func TestTopBar_HexUnknownModelHidesPercent(t *testing.T) {
	tr := cost.New()
	tr.Record("ollama", "weird-local-model", 1000, 500)
	m := NewModel(nil, "/tmp/work", "weird-local-model", "workspace-write", caveman.Default).WithCostTracker(tr)
	m.width = 120
	out := stripANSI(m.renderTopBar())
	if strings.Contains(out, "%") {
		t.Fatalf("unknown context window should suppress percent: %q", out)
	}
	if !strings.Contains(out, "🐝") {
		t.Fatalf("expected bee glyph when window unknown: %q", out)
	}
}

func TestCostFlash_DoesntDoubleFire(t *testing.T) {
	tr := cost.New()
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(tr)
	tr.Record("openai", "gpt-4o-mini", 1000, 500)
	if cmd := m.maybeStartCostFlash(); cmd == nil {
		t.Fatal("first call should arm flash")
	}
	if cmd := m.maybeStartCostFlash(); cmd != nil {
		t.Fatal("second call with no new events should be a no-op")
	}
}
