package loop

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// one dispatch batch is one step: eight parallel reads that all miss must not
// trip the per-tool failure cap, which counts consecutive steps.
func TestEngineRun_ParallelBatchFailuresCountOnce(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "read",
		desc: "read",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{}, errors.New("no such file")
		},
	})
	var evs []llm.Event
	for i := range 8 {
		evs = append(evs, llm.Event{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
			ID: fmt.Sprintf("u-%d", i), Name: "read", Input: map[string]any{"path": fmt.Sprintf("guess-%d.go", i)},
		}})
	}
	evs = append(evs, llm.Event{Type: llm.EventDone, StopReason: "tool_use"})
	p := &stubProvider{scripts: [][]llm.Event{evs, {{Type: llm.EventTextDelta, Delta: "found it"}, {Type: llm.EventDone}}}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(context.Background(), "find the file")
	if err != nil {
		t.Fatalf("one batch of failing reads must not bail the run: %v", err)
	}
	if res.FinalText != "found it" {
		t.Fatalf("final text = %q", res.FinalText)
	}
}

func TestRepeatTrackerBatchCountsOncePerTool(t *testing.T) {
	tr := newRepeatTracker()
	tr.beginBatch()
	var obs Observation
	for i := range 8 {
		obs = tr.Observe(types.ToolUse{Name: "read", Input: map[string]any{"path": fmt.Sprint(i)}}, true)
	}
	tr.endBatch()
	if obs.ConsecutiveSameToolFailures != 1 {
		t.Fatalf("streak after one batch = %d, want 1", obs.ConsecutiveSameToolFailures)
	}
	tr.beginBatch()
	obs = tr.Observe(types.ToolUse{Name: "read", Input: map[string]any{"path": "x"}}, true)
	tr.endBatch()
	if obs.ConsecutiveSameToolFailures != 2 {
		t.Fatalf("streak after second batch = %d, want 2", obs.ConsecutiveSameToolFailures)
	}
}
