package loop

import (
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/types"
)

// minimal engine for warning-injection tests. config is the only field
// injectIterAndTokenWarnings actually consults beyond the warned* flags.
func warnEngine(t int) *Engine {
	cfg := config.Defaults()
	cfg.Profile = "tiny"
	if t > 0 {
		// override the tiny default (3) so the test can pin the threshold.
		prof := cfg.Profiles["tiny"]
		prof.NoMutationStallThreshold = t
		cfg.Profiles["tiny"] = prof
	}
	return &Engine{Cfg: cfg}
}

func extractWarnText(blocks []types.ContentBlock) string {
	var b strings.Builder
	for _, blk := range blocks {
		b.WriteString(blk.Text)
		if blk.Result != nil {
			b.WriteString(blk.Result.Content)
		}
	}
	return b.String()
}

func TestStallWarning_FirstNudge(t *testing.T) {
	e := warnEngine(3)
	e.noMutationStreak = 3
	blocks := []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "ok"}},
	}
	out := injectIterAndTokenWarnings(e, blocks, 1, 50, 0)
	text := extractWarnText(out)
	if !strings.Contains(text, "[stall] 3 read-only iters") {
		t.Errorf("expected first stall nudge, got: %q", text)
	}
	if strings.Contains(text, "previous nudge ignored") {
		t.Error("escalation should NOT fire on first nudge")
	}
	if !e.warnedStall {
		t.Error("warnedStall flag not set")
	}
}

func TestStallWarning_EscalateAfterIgnored(t *testing.T) {
	e := warnEngine(3)
	e.warnedStall = true // first nudge already fired
	e.noMutationStreak = 6 // 2x threshold
	blocks := []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "ok"}},
	}
	out := injectIterAndTokenWarnings(e, blocks, 1, 50, 0)
	text := extractWarnText(out)
	if !strings.Contains(text, "previous nudge ignored") {
		t.Errorf("expected escalation nudge, got: %q", text)
	}
	if !strings.Contains(text, "call `escalate`") {
		t.Errorf("expected escalate pointer in nudge, got: %q", text)
	}
	if !e.warnedStallEscalate {
		t.Error("warnedStallEscalate flag not set")
	}
}

func TestStallWarning_EscalateOnceOnly(t *testing.T) {
	e := warnEngine(3)
	e.warnedStall = true
	e.warnedStallEscalate = true // already fired
	e.noMutationStreak = 7
	blocks := []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "ok"}},
	}
	out := injectIterAndTokenWarnings(e, blocks, 1, 50, 0)
	text := extractWarnText(out)
	if strings.Contains(text, "previous nudge ignored") {
		t.Error("escalation should not fire twice")
	}
}

func TestStallWarning_CatchupFiresBoth(t *testing.T) {
	// when streak is already at 2x but no nudge has fired yet (eg first
	// iter check after a long wait), fire BOTH the soft nudge and the
	// escalation pointer in the same call. better than one-per-call when
	// the model is already deep in the stall.
	e := warnEngine(3)
	e.warnedStall = false
	e.noMutationStreak = 6
	blocks := []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "ok"}},
	}
	out := injectIterAndTokenWarnings(e, blocks, 1, 50, 0)
	text := extractWarnText(out)
	if !strings.Contains(text, "[stall]") {
		t.Error("expected first-tier stall nudge")
	}
	if !strings.Contains(text, "previous nudge ignored") {
		t.Error("expected escalation to also fire when already past 2x threshold")
	}
}
