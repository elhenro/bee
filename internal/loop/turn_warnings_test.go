package loop

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/types"
)

// run is allocated so the warned* flag writes don't nil-panic.
func warnEngine() *Engine {
	return &Engine{Cfg: config.Defaults(), run: &runState{}}
}

func warnText(blocks []types.ContentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
		if blk.Result != nil {
			b.WriteString(blk.Result.Content)
		}
	}
	return b.String()
}

func resultBlocks() []types.ContentBlock {
	return []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "ok"}}}
}

func TestIterWarnings_HalfThenEightyOnce(t *testing.T) {
	e := warnEngine()
	if got := warnText(injectIterAndTokenWarnings(e, resultBlocks(), 4, 10, 0)); strings.Contains(got, "[iter") {
		t.Fatalf("warned before half: %q", got)
	}
	got := warnText(injectIterAndTokenWarnings(e, resultBlocks(), 5, 10, 0))
	if !strings.Contains(got, "[iter 5/10] half the budget spent") {
		t.Fatalf("expected half warning, got %q", got)
	}
	if got := warnText(injectIterAndTokenWarnings(e, resultBlocks(), 6, 10, 0)); strings.Contains(got, "[iter") {
		t.Fatalf("half warning fired twice: %q", got)
	}
	got = warnText(injectIterAndTokenWarnings(e, resultBlocks(), 8, 10, 0))
	if !strings.Contains(got, "[iter 8/10] near iter cap") {
		t.Fatalf("expected eighty warning, got %q", got)
	}
}

func TestIterWarnings_SkippedWhenCapLifted(t *testing.T) {
	e := warnEngine()
	if got := warnText(injectIterAndTokenWarnings(e, resultBlocks(), 500, 0, 0)); strings.Contains(got, "[iter") {
		t.Fatalf("iter warning with no cap: %q", got)
	}
}

func TestTokenWarnings_FireOnBudgetCrossing(t *testing.T) {
	e := warnEngine()
	e.run.cumInputTokens = 400
	e.run.cumOutputTokens = 100
	got := warnText(injectIterAndTokenWarnings(e, resultBlocks(), 1, 50, 1000))
	if !strings.Contains(got, "[tokens 500/1000] half the token budget") {
		t.Fatalf("expected half token warning, got %q", got)
	}
	e.run.cumInputTokens = 850
	got = warnText(injectIterAndTokenWarnings(e, resultBlocks(), 1, 50, 1000))
	if !strings.Contains(got, "near token cap") {
		t.Fatalf("expected eighty token warning, got %q", got)
	}
}

func TestReasoningDupWarning_FiresOnce(t *testing.T) {
	e := warnEngine()
	e.run.reasoningDupStreak = reasoningDupBailAt
	got := warnText(injectIterAndTokenWarnings(e, resultBlocks(), 1, 50, 0))
	if !strings.Contains(got, "near-identical reasoning") {
		t.Fatalf("expected reasoning loop warning, got %q", got)
	}
	if got := warnText(injectIterAndTokenWarnings(e, resultBlocks(), 1, 50, 0)); strings.Contains(got, "near-identical") {
		t.Fatalf("reasoning loop warning fired twice: %q", got)
	}
}
