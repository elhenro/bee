package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// a context window learned mid-Run (the hosted probe lands late) arms the token
// budget for the rest of that Run, not only the next one.
func TestTokenBudgetArmsWhenWindowArrivesMidRun(t *testing.T) {
	const model = "mystery-model-late-window"
	t.Cleanup(llm.ResetLiveContextLengths)
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "bash", desc: "x", fn: func(context.Context, map[string]any) (tools.Result, error) {
		llm.RememberContextLength(model, 65536) // the probe answers during the first tool call
		return tools.Result{Content: "ok"}, nil
	}})
	p := &stubProvider{scripts: [][]llm.Event{{
		{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u", Name: "bash", Input: map[string]any{"command": "x"}}},
		{Type: llm.EventDone, StopReason: "tool_use", Usage: &llm.Usage{InputTokens: 200000, OutputTokens: 50000}},
	}}}
	eng, _ := newEngine(p, reg)
	eng.Cfg.DefaultModel = model
	eng.Cfg.Providers = nil // no probe target: the window only arrives via the tool
	eng.Cfg.Compaction.Enabled = false
	_, err := eng.Run(context.Background(), "loop me")
	if err == nil || !strings.Contains(err.Error(), "token budget") {
		t.Fatalf("err = %v, want a token-budget stop once the window is known", err)
	}
	if got := p.calls.Load(); got > 10 {
		t.Errorf("provider calls = %d, budget should stop the Run within a few iterations", got)
	}
}
