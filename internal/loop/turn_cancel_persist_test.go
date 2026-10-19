package loop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// a cancel that lands while the only tool runs must still persist its
// tool_result, or the rollout keeps an assistant tool_use with no answer.
func TestRun_CancelDuringLastToolPersistsResult(t *testing.T) {
	t.Setenv("BEE_SESSIONS_DIR", t.TempDir())
	started := make(chan struct{})
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "shell",
		fn: func(ctx context.Context, _ map[string]any) (tools.Result, error) {
			close(started)
			<-ctx.Done()
			return tools.Result{Content: "signal: killed", IsError: true}, nil
		},
	})
	p := &stubProvider{scripts: [][]llm.Event{{
		{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u1", Name: "bash", Input: map[string]any{"command": "sleep 100"}}},
		{Type: llm.EventDone, StopReason: "tool_use"},
	}}}
	eng, _ := newEngine(p, reg)
	roll, err := session.Open("cancel-last-tool")
	if err != nil {
		t.Fatal(err)
	}
	eng.Sessions = roll
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; time.Sleep(20 * time.Millisecond); cancel() }()
	res, err := eng.Run(ctx, "run it")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	_ = roll.Close()
	disk, err := session.Read("cancel-last-tool")
	if err != nil {
		t.Fatal(err)
	}
	if len(disk) != 3 || disk[2].Role != types.RoleTool || !hasToolResult(disk[2]) {
		t.Fatalf("rollout must end with the tool_result message, got %d msgs", len(disk))
	}
	if last := res.Messages[len(res.Messages)-1]; last.Role != types.RoleTool {
		t.Fatalf("in-memory history ends with %s, want tool", last.Role)
	}
}
