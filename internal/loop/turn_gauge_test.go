package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// the between-gauge excludes tool time: a 60ms tool must not show up as harness
// time, and each iteration reports exactly one gauge.
func TestHarnessGaugeExcludesToolTime(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "bash", desc: "x", fn: func(context.Context, map[string]any) (tools.Result, error) {
		time.Sleep(60 * time.Millisecond)
		return tools.Result{Content: "ok"}, nil
	}})
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u1", Name: "bash", Input: map[string]any{"command": "x"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)
	var out bytes.Buffer
	eng.JSONEmitter = jsonmode.New(&out)
	if _, err := eng.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var first, between []float64
	dec := json.NewDecoder(&out)
	for dec.More() {
		var ev jsonmode.Event
		if err := dec.Decode(&ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type != "harness" || ev.Ms == nil {
			continue
		}
		switch ev.Name {
		case "first":
			first = append(first, *ev.Ms)
		case "between":
			between = append(between, *ev.Ms)
		}
	}
	if len(first) != 1 || len(between) != 1 {
		t.Fatalf("gauges first=%v between=%v, want one each", first, between)
	}
	if between[0] > 30 {
		t.Errorf("between gauge = %.1fms with a 60ms tool: tool time leaked into harness time", between[0])
	}
}
