package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/types"
)

func thoughtMsg(text string, ms int64) types.Message {
	return types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockThinking, Text: text, ThinkMs: ms}},
	}
}

func TestThinking_HiddenKeepsTimeDropsText(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetShowThoughts(false)
	got := stripANSI(r.RenderMessage(thoughtMsg("secret plan", 4200)))
	if !strings.Contains(got, "Thought for 4s") {
		t.Fatalf("hidden thoughts should still show the time: %q", got)
	}
	if strings.Contains(got, "secret plan") {
		t.Fatalf("hidden thoughts leaked their text: %q", got)
	}
}

func TestThinking_ShownCarriesTimeAndText(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetShowThoughts(true)
	got := stripANSI(r.RenderMessage(thoughtMsg("secret plan", 1500)))
	for _, want := range []string{"Thought for 1s", "secret plan"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %q", want, got)
		}
	}
}

// sessions saved before timing existed have no duration to show
func TestThinking_UntimedBlockHiddenStaysSilent(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetShowThoughts(false)
	if got := r.RenderMessage(thoughtMsg("old reasoning", 0)); got != "" {
		t.Fatalf("untimed hidden block should render nothing: %q", got)
	}
}

func TestThinking_TimedWithoutTextStillShowsLabel(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), 80)
	r.SetShowThoughts(true)
	got := stripANSI(r.RenderMessage(thoughtMsg("", 900)))
	if !strings.Contains(got, "Thought for 0s") {
		t.Fatalf("signature-only block lost its label: %q", got)
	}
}

func TestRenderLive_HiddenThoughtsShowRunningClock(t *testing.T) {
	m := streamingModel(t)
	m.showThoughts = false
	m.stream.SetShowThoughts(false)
	m.thinkPartial = "private chain of thought"
	m.thinkStartedAt = time.Now().Add(-3 * time.Second)
	out := stripANSI(m.renderLive(14))
	if !strings.Contains(out, "Thinking…") {
		t.Fatalf("live row should say Thinking while the clock runs: %q", out)
	}
	if strings.Contains(out, "private chain") {
		t.Fatalf("hidden thoughts leaked into the live region: %q", out)
	}
}

func TestRenderLive_ClockFreezesWhenAnswerStarts(t *testing.T) {
	m := streamingModel(t)
	m.showThoughts = false
	m.stream.SetShowThoughts(false)
	m.thinkPartial = "hm"
	m.thinkStartedAt = time.Now().Add(-5 * time.Second)
	m.answerStarted()
	d, running := m.thinkElapsed()
	if running {
		t.Fatal("clock should stop at the first answer delta")
	}
	if d < 4*time.Second || d > 6*time.Second {
		t.Fatalf("frozen duration %v, want ~5s", d)
	}
	if out := stripANSI(m.renderLive(14)); !strings.Contains(out, "Thought for") {
		t.Fatalf("live row should read Thought for once frozen: %q", out)
	}
}

func TestThinkingLabel_FitsNarrowTerminal(t *testing.T) {
	r := NewStreamRenderer(DefaultStyles(), minRenderWidth)
	r.SetShowThoughts(false)
	for _, ln := range strings.Split(stripANSI(r.RenderMessage(thoughtMsg("x", 125_000))), "\n") {
		if w := lipgloss.Width(ln); w > minRenderWidth {
			t.Fatalf("row %d cols exceeds %d: %q", w, minRenderWidth, ln)
		}
	}
}
