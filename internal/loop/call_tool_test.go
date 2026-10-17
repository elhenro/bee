package loop

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

type echoTool struct{}

func (echoTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: "echo", Description: "echo", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"},
	}}
}

func (echoTool) Run(_ context.Context, in map[string]any) (tools.Result, error) {
	return tools.Result{Content: "echo:" + in["text"].(string)}, nil
}

func TestCallTool_RunsOutsideTurnAndIgnoresTurnGate(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(echoTool{}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Tools: reg, Cfg: config.Defaults()}
	// a stale per-turn gate that excludes echo must not block an out-of-turn call.
	e.run = &runState{allowedTools: map[string]bool{"read": true}}
	res, err := e.CallTool(context.Background(), "echo", map[string]any{"text": "hi"})
	if err != nil || res.IsError || res.Content != "echo:hi" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if e.run == nil || !e.run.allowedTools["read"] {
		t.Fatal("turn state not restored")
	}
	if res, _ := e.CallTool(context.Background(), "nope", nil); !res.IsError {
		t.Fatalf("unknown tool should error: %+v", res)
	}
}
