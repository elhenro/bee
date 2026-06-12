package tui

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/llm"
)

// fresh TUI, no events recorded: the hex chip is just the bee, no %.
func TestRenderContextHex_Fresh(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(cost.New())
	got := stripANSI(m.renderContextHex())
	if strings.Contains(got, "%") || strings.Contains(got, "?") {
		t.Errorf("fresh: hex should not show %% or ?, got %q", got)
	}
	if !strings.Contains(got, "🐝") {
		t.Errorf("fresh: hex should show bee, got %q", got)
	}
}

// known model + recorded events: percent renders, scales with the cap.
func TestRenderContextHex_KnownModel(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	tr := cost.New()
	tr.Record("openai", "gpt-4o-mini", 64_000, 1_000) // ~50% of 128k
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(tr)
	got := stripANSI(m.renderContextHex())
	if !strings.Contains(got, "50%") {
		t.Errorf("known model: hex should show 50%%, got %q", got)
	}
}

// unknown model (id not in hardcoded table, no /v1/models surface) with
// recorded events: hex renders a "?" so the user notices the bar is silent.
func TestRenderContextHex_UnknownModelWithEvents(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	// deliberately not in models_hardcoded.go; if the curated table grows to
	// cover this id, the test starts passing the "known" path and needs to
	// be repointed at a still-unknown id.
	const modelID = "minimax/MiniMax-Z999-future"
	tr := cost.New()
	tr.Record("openrouter", modelID, 12_345, 1_000)
	m := NewModel(nil, "/tmp/work", modelID, "workspace-write", caveman.Default).WithCostTracker(tr)
	got := stripANSI(m.renderContextHex())
	if !strings.Contains(got, "?") {
		t.Errorf("unknown model with events: hex should show ?, got %q", got)
	}
	if strings.Contains(got, "%") {
		t.Errorf("unknown model: hex should not show %%, got %q", got)
	}
	if !strings.Contains(got, "🐝") {
		t.Errorf("unknown model: hex should still show bee, got %q", got)
	}
}

// once ContextWindow learns the unknown id (e.g. via the curated table, or
// the runtime probe), the hex recovers and shows a real % — proves the
// unknown path is just a fallback, not a permanent state.
func TestRenderContextHex_RecoversWhenWindowLearned(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	llm.RememberContextLength("minimax/MiniMax-M3", 200000)
	tr := cost.New()
	tr.Record("openrouter", "minimax/MiniMax-M3", 100_000, 1_000) // 50% of 200k
	m := NewModel(nil, "/tmp/work", "minimax/MiniMax-M3", "workspace-write", caveman.Default).WithCostTracker(tr)
	got := stripANSI(m.renderContextHex())
	if strings.Contains(got, "?") {
		t.Errorf("learned: hex should not show ?, got %q", got)
	}
	if !strings.Contains(got, "50%") {
		t.Errorf("learned: hex should show 50%%, got %q", got)
	}
}

// fresh state: bar is the quiet oyster rule, all ─ chars, width preserved.
func TestRenderContextBar_Fresh(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(cost.New())
	m.width = 40
	got := stripANSI(m.renderContextBar())
	// stripANSI leaves raw runes; the bar should be 40 ─ glyphs.
	if r := []rune(got); len(r) != 40 {
		t.Errorf("fresh: bar width = %d, want 40", len(r))
	}
	for _, r := range got {
		if r != '─' {
			t.Errorf("fresh: bar should be all ─, got %q (rune %q)", got, r)
			break
		}
	}
}

// known model + events: bar fills proportionally to pct of cap.
func TestRenderContextBar_KnownModelFills(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	tr := cost.New()
	tr.Record("openai", "gpt-4o-mini", 32_000, 1_000) // 25% of 128k
	m := NewModel(nil, "/tmp/work", "gpt-4o-mini", "workspace-write", caveman.Default).WithCostTracker(tr)
	m.width = 40
	got := stripANSI(m.renderContextBar())
	// 25% of 40 = 10 fill chars (━), 30 rest chars (─).
	fills := strings.Count(got, "━")
	rests := strings.Count(got, "─")
	if fills != 10 {
		t.Errorf("known 25%%: fill count = %d, want 10", fills)
	}
	if rests != 30 {
		t.Errorf("known 25%%: rest count = %d, want 30", rests)
	}
}

// unknown model + events: bar drops a single "?" on the left, the rest is the
// quiet rule. Width is preserved (one rune for "?", width-1 for the dashes).
func TestRenderContextBar_UnknownModelShowsQuestion(t *testing.T) {
	llm.ResetLiveContextLengths()
	defer llm.ResetLiveContextLengths()
	// deliberately not in models_hardcoded.go.
	const modelID = "minimax/MiniMax-Z999-future"
	tr := cost.New()
	tr.Record("openrouter", modelID, 12_345, 1_000)
	m := NewModel(nil, "/tmp/work", modelID, "workspace-write", caveman.Default).WithCostTracker(tr)
	m.width = 40
	got := stripANSI(m.renderContextBar())
	if !strings.HasPrefix(got, "?") {
		t.Errorf("unknown: bar should start with ?, got %q", got)
	}
	if r := []rune(got); len(r) != 40 {
		t.Errorf("unknown: bar width = %d, want 40", len(r))
	}
	// no ━ — the fill is suppressed because we don't know the cap.
	if strings.Contains(got, "━") {
		t.Errorf("unknown: bar must not show ━ fill, got %q", got)
	}
}

// zero width: bar returns empty (caller can't compute fill). No crash.
func TestRenderContextBar_ZeroWidthIsEmpty(t *testing.T) {
	m := NewModel(nil, "/tmp/work", "x", "workspace-write", caveman.Default).WithCostTracker(cost.New())
	if got := m.renderContextBar(); got != "" {
		t.Errorf("zero width: got %q, want empty", got)
	}
}
