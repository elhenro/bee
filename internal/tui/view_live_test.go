package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/types"
)

func streamingModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.state = StateStreaming
	m.turnStartedAt = time.Now().Add(-5 * time.Second)
	m.showLoaderIn, m.showLoaderOut, m.showLoaderRate = true, true, true
	return m
}

func TestRenderLive_IdleIsEmpty(t *testing.T) {
	if got := newTestModel(t).renderLive(20); got != "" {
		t.Fatalf("idle live region should collapse, got %q", got)
	}
}

func TestRenderLive_PreTokenShowsOnlyBusyRow(t *testing.T) {
	m := streamingModel(t)
	out := stripANSI(m.renderLive(20))
	if !strings.Contains(out, m.stream.spinnerVerb()+"…") || !strings.Contains(out, interruptHint) {
		t.Fatalf("busy row missing: %q", out)
	}
	var rows int
	for _, ln := range strings.Split(out, "\n") {
		if strings.TrimSpace(ln) != "" {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("no answer text yet, only the busy row, got %d rows: %q", rows, out)
	}
}

func TestRenderLive_StreamsTextAboveBusyRow(t *testing.T) {
	m := streamingModel(t)
	m.partial = "working on it"
	out := stripANSI(m.renderLive(20))
	ti, bi := strings.Index(out, "working on it"), strings.Index(out, interruptHint)
	if ti < 0 || bi < 0 || ti > bi {
		t.Fatalf("text must sit above the busy row: %q", out)
	}
}

func TestRenderLive_PendingToolsSitBetweenTextAndBusyRow(t *testing.T) {
	m := streamingModel(t)
	m.partial = "reading"
	m.pendingTools = []types.ToolUse{{ID: "u1", Name: "read", Input: map[string]any{"path": "a.go"}}}
	out := stripANSI(m.renderLive(20))
	ti, pi, bi := strings.Index(out, "reading"), strings.Index(out, "Read  a.go"), strings.Index(out, interruptHint)
	if !(ti >= 0 && ti < pi && pi < bi) {
		t.Fatalf("want text, then running call, then busy row: %q", out)
	}
}

func TestRenderLive_FlushedHeadContinuesWithoutMarker(t *testing.T) {
	m := streamingModel(t)
	m.partial = "head paragraph\n\ntail still streaming"
	m.streamFlushed = "head paragraph\n\n"
	out := stripANSI(m.renderLive(20))
	if strings.Contains(out, glyphBee+" tail") {
		t.Fatalf("flushed head already printed the marker: %q", out)
	}
	if strings.Contains(out, "head paragraph") {
		t.Fatalf("flushed prefix must not render twice: %q", out)
	}
	if !strings.Contains(out, "tail still streaming") {
		t.Fatalf("tail missing: %q", out)
	}
}

func TestRenderLive_CompactingShowsLabelledSpinner(t *testing.T) {
	m := newTestModel(t)
	m.compacting = true
	if out := stripANSI(m.renderLive(20)); !strings.Contains(out, "Compacting context…") {
		t.Fatalf("compaction row missing: %q", out)
	}
	m.compacting, m.handoffing = false, true
	if out := stripANSI(m.renderLive(20)); !strings.Contains(out, "Writing handoff…") {
		t.Fatalf("handoff row missing: %q", out)
	}
}

func TestRenderLive_PinnedLoaderKeepsLegacyLayout(t *testing.T) {
	m := streamingModel(t)
	m.stream.SetLoaderStyle(LoaderStyleSwarm)
	out := stripANSI(m.renderLive(20))
	if strings.Contains(out, interruptHint) {
		t.Fatalf("pinned braille loaders don't get the busy row: %q", out)
	}
	if out == "" {
		t.Fatal("pinned loader should still draw its animation")
	}
}

func TestBusyRows_ReservesSpinnerAndRunningCalls(t *testing.T) {
	m := streamingModel(t)
	if got := m.busyRows(); got != 2 {
		t.Fatalf("clean: busy row + blank = 2, got %d", got)
	}
	m.pendingTools = []types.ToolUse{
		{ID: "1", Name: "read", Input: map[string]any{"path": "a"}},
		{ID: "2", Name: "read", Input: map[string]any{"path": "b"}},
	}
	if got := m.busyRows(); got != 4 {
		t.Fatalf("two running calls add two rows, got %d", got)
	}
	m.stream.SetLoaderStyle(LoaderStyleSwarm)
	if got := m.busyRows(); got != 0 {
		t.Fatalf("pinned loaders reserve nothing, got %d", got)
	}
}

func TestView_StreamingFrameOrder(t *testing.T) {
	m := streamingModel(t)
	m.partial = "answer"
	out := stripANSI(m.View())
	idx := func(s string) int { return strings.Index(out, s) }
	if !(idx("answer") < idx(interruptHint) && idx(interruptHint) < idx(glyphPrompt) && idx(glyphPrompt) < idx("test-model")) {
		t.Fatalf("frame order should be text, busy row, prompt, footer: %q", out)
	}
}

func TestRenderLive_StripLoaderKeepsTokenReadout(t *testing.T) {
	m := streamingModel(t)
	m.stream.SetLoaderStyle(LoaderStyleStrip)
	m.partial = "some answer text"
	m.turnOutChars = 400
	out := stripANSI(m.renderLive(20))
	if !strings.Contains(out, "some answer text") || !strings.Contains(out, "↓") {
		t.Fatalf("strip style should keep text + token readout below it: %q", out)
	}
	if strings.Contains(out, interruptHint) {
		t.Fatalf("strip style has no busy row: %q", out)
	}
}

func TestParseLoaderStyle_SpinnerStripPhased(t *testing.T) {
	cases := map[string]LoaderStyle{
		"":        LoaderStyleDefault,
		"spinner": LoaderStyleDefault,
		"strip":   LoaderStyleStrip,
		"tokens":  LoaderStyleStrip,
		"phased":  LoaderStylePhased,
		"braille": LoaderStylePhased,
	}
	for in, want := range cases {
		if got := ParseLoaderStyle(in); got != want {
			t.Errorf("ParseLoaderStyle(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestResize_KeepsToolCallIndexForLateResults(t *testing.T) {
	m := newTestModel(t)
	m.stream.RenderMessage(useMsg("u1", "bash", map[string]any{"command": "make"}))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = updated.(Model)
	out := stripANSI(m.stream.RenderMessage(resultMsg("u1", "ok", false)))
	if !strings.Contains(out, "Bash  make") {
		t.Fatalf("result after a resize lost its call header: %q", out)
	}
}

func TestRenderLive_LongReasoningStaysInsideBudget(t *testing.T) {
	m := streamingModel(t)
	m.showThoughts = true
	var b strings.Builder
	for i := 0; i < 200; i++ {
		b.WriteString("thinking about the next step\n")
	}
	m.thinkPartial = b.String()
	rows := strings.Count(m.renderLive(14), "\n") + 1
	if rows > 14 {
		t.Fatalf("live region is %d rows, budget 14", rows)
	}
	if out := stripANSI(m.renderLive(14)); !strings.Contains(out, "lines above") {
		t.Fatalf("clipped reasoning should say how much is hidden: %q", out)
	}
	m.partial = "and the answer so far"
	if rows := strings.Count(m.renderLive(14), "\n") + 1; rows > 14 {
		t.Fatalf("with text too: %d rows, budget 14", rows)
	}
}

func TestSetLoader_ConfigPicksTheStyleAndTheEnvWins(t *testing.T) {
	t.Setenv("BEE_LOADER", "")
	r := NewStreamRenderer(DefaultStyles(), 80)
	if !r.isDefaultLoader() {
		t.Fatal("no config and no env should give the busy-row spinner")
	}
	r.SetLoader("strip")
	if !r.isStripLoader() {
		t.Fatal("tui.loader = strip should pin the strip")
	}
	r.SetLoader("not-a-loader")
	if !r.isDefaultLoader() {
		t.Fatal("an unknown name falls back to the spinner")
	}
	t.Setenv("BEE_LOADER", "phased")
	r.SetLoader("strip")
	if r.loaderStyle != LoaderStylePhased {
		t.Fatalf("BEE_LOADER should win over config, got style %v", r.loaderStyle)
	}
}

// a resize rebuilds the renderer; the configured loader must survive it
func TestLoader_SurvivesResize(t *testing.T) {
	t.Setenv("BEE_LOADER", "")
	m := newTestModel(t).WithLoader("strip")
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if !next.(Model).stream.isStripLoader() {
		t.Fatal("resize dropped the configured loader")
	}
}
