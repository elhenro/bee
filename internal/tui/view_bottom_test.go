package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/llm"
)

func TestBottomBar_FramesInputBetweenRules(t *testing.T) {
	m := newTestModel(t)
	rows := strings.Split(stripANSI(m.renderBottomBar()), "\n")
	if len(rows) != 4 {
		t.Fatalf("want blank + rule + input + rule, got %d rows: %q", len(rows), rows)
	}
	if rows[0] != "" {
		t.Fatalf("clean layout opens with a breathing row: %q", rows[0])
	}
	rule := " " + strings.Repeat(glyphRule, m.ruleWidth())
	if rows[1] != rule || rows[3] != rule {
		t.Fatalf("input must sit between two hairlines: %q", rows)
	}
	if !strings.HasPrefix(rows[2], promptFirst) || !strings.Contains(rows[2], "Ask anything") {
		t.Fatalf("input row should lead with the chevron + placeholder: %q", rows[2])
	}
}

func TestBottomBar_CompactSkipsBreathingRow(t *testing.T) {
	m := newTestModel(t).WithCompact(true)
	if out := m.renderBottomBar(); strings.HasPrefix(out, "\n") {
		t.Fatalf("compact bottom bar must not open with a blank row: %q", out)
	}
}

func TestInput_ShellModeSwapsPrompt(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("!ls")
	row := stripANSI(m.renderInput())
	if !strings.HasPrefix(row, promptShell) {
		t.Fatalf("shell mode prompt should be %q, got %q", promptShell, row)
	}
	m.input.SetValue("hello")
	if row := stripANSI(m.renderInput()); !strings.HasPrefix(row, promptFirst) {
		t.Fatalf("chat prompt should be %q, got %q", promptFirst, row)
	}
}

func TestInput_ContinuationRowsHangUnderPrompt(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("one\ntwo")
	m.input.SetHeight(2)
	rows := strings.Split(stripANSI(m.renderInput()), "\n")
	if !strings.HasPrefix(rows[0], promptFirst+"one") || !strings.HasPrefix(rows[1], promptRest+"two") {
		t.Fatalf("multi-line input should hang under the chevron: %q", rows)
	}
}

func TestHelpLine_WrapsToWidth(t *testing.T) {
	m := newTestModel(t)
	m.width = 44
	rows := strings.Split(stripANSI(m.renderHelpLine()), "\n")
	if len(rows) < 3 {
		t.Fatalf("help should wrap on a narrow terminal, got %d row(s)", len(rows))
	}
	for _, r := range rows {
		if lipgloss.Width(r) > m.width {
			t.Fatalf("help row wider than the terminal: %q", r)
		}
	}
	m.width = 400
	if one := strings.Split(stripANSI(m.renderHelpLine()), "\n"); len(one) != 1 {
		t.Fatalf("help fits one row on a wide terminal, got %d", len(one))
	}
}

func TestStatusLine_RightGroupHugsEdge(t *testing.T) {
	m := statusTestModel()
	out := stripANSI(m.renderTopBar())
	if lipgloss.Width(out) != m.width-1 {
		t.Fatalf("footer should span the row minus the right margin, got %d of %d: %q", lipgloss.Width(out), m.width, out)
	}
	if !strings.HasSuffix(strings.TrimRight(out, " "), "? help") {
		t.Fatalf("hints belong at the far right: %q", out)
	}
}

func TestStatusLine_ShedsLowPriorityItemsFirst(t *testing.T) {
	m := statusTestModel()
	m.width = 58
	out := stripANSI(m.renderTopBar())
	if strings.Contains(out, "commands") {
		t.Fatalf("hints go first when narrow: %q", out)
	}
	for _, keep := range []string{"gpt-4o-mini", "worker"} {
		if !strings.Contains(out, keep) {
			t.Fatalf("identity item %q must survive: %q", keep, out)
		}
	}
	if lipgloss.Width(out) > m.width {
		t.Fatalf("footer wider than the terminal: %q", out)
	}
}

func TestStatusHints_FollowInputState(t *testing.T) {
	m := statusTestModel()
	if h := stripANSI(drawStatusHints(m)); !strings.Contains(h, "commands") {
		t.Fatalf("idle hints should teach / and @: %q", h)
	}
	m.input.SetValue("hi")
	if h := stripANSI(drawStatusHints(m)); !strings.Contains(h, "send") || !strings.Contains(h, "newline") {
		t.Fatalf("typing hints should show send/newline: %q", h)
	}
	m.state = StateStreaming
	if h := drawStatusHints(m); h != "" {
		t.Fatalf("mid-turn the busy row owns the hint: %q", h)
	}
	m.state = StateIdle
	m.palette.Active = true
	if h := drawStatusHints(m); h != "" {
		t.Fatalf("hints hide while a picker owns the keys: %q", h)
	}
}

func TestContextTier(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	cases := []struct {
		pct  float64
		want lipgloss.TerminalColor
		bold bool
	}{
		{0.10, fgSquid, false}, {0.60, accentHoney, false}, {0.90, accentBusy, true}, {0.99, semError, true},
	}
	for _, c := range cases {
		got, bold := contextTier(c.pct, true)
		if got != c.want || bold != c.bold {
			t.Errorf("contextTier(%v) = %v/%v, want %v/%v", c.pct, got, bold, c.want, c.bold)
		}
	}
	if got, _ := contextTier(0.99, false); got != fgSquid {
		t.Error("unknown window never alarms")
	}
}

func TestContextChip_ShowsLabelAndPercent(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	tr := cost.New()
	tr.Record("openai", "gpt-4o-mini", 64_000, 1_000)
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(tr)
	if got := stripANSI(m.renderContextHex()); got != glyphBee+" ctx 50%" {
		t.Fatalf("chip = %q", got)
	}
}
