package loop

import (
	"context"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

type chattyTool struct{}

func (chattyTool) Spec() llm.ToolSpec {
	return llm.ToolSpec{Name: "chatty", Description: "chatty", Schema: map[string]any{"type": "object"}}
}

func (chattyTool) Run(ctx context.Context, _ map[string]any) (tools.Result, error) {
	if sink := tools.LiveSink(ctx); sink != nil {
		sink("working")
	}
	return tools.Result{Content: "done"}, nil
}

func TestToolTail_ReachesChannelTaggedWithUseID(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(chattyTool{}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Tools: reg, Cfg: config.Defaults(), ToolTailCh: make(chan ToolTail, 4)}
	res, err := e.CallTool(context.Background(), "chatty", nil)
	if err != nil || res.IsError {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	select {
	case got := <-e.ToolTailCh:
		if got.UseID != res.UseID || got.Text != "working" {
			t.Fatalf("got %+v, want use %q", got, res.UseID)
		}
	default:
		t.Fatal("no tail delivered")
	}
}

func TestToolTail_NilChannelLeavesToolsSilent(t *testing.T) {
	reg := tools.NewRegistry()
	if err := reg.Register(chattyTool{}); err != nil {
		t.Fatal(err)
	}
	e := &Engine{Tools: reg, Cfg: config.Defaults()}
	if res, err := e.CallTool(context.Background(), "chatty", nil); err != nil || res.IsError {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
