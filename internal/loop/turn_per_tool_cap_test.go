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

// 8 consecutive failures on the same tool name (different args each time, so
// the sig-level two-strike never fires) must trip ErrPerToolFailureCap. catches
// the "model wedged on bash, keeps trying different commands that all fail"
// scenario where two-strike wouldn't fire.
func TestEngineRun_PerToolFailureCapAtEight(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "shell",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{}, errors.New("simulated bash failure")
		},
	})
	// 8 distinct-args calls — different sig each iter so two-strike streak
	// stays at 1, only the per-tool-name streak climbs.
	mkScript := func(i int) []llm.Event {
		return []llm.Event{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
				ID:    fmt.Sprintf("u-%d", i),
				Name:  "bash",
				Input: map[string]any{"command": fmt.Sprintf("cmd-%d", i)},
			}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		}
	}
	scripts := make([][]llm.Event, 8)
	for i := 0; i < 8; i++ {
		scripts[i] = mkScript(i)
	}
	p := &stubProvider{scripts: scripts}
	eng, _ := newEngine(p, reg)

	_, err := eng.Run(context.Background(), "go")
	if err == nil {
		t.Fatalf("expected ErrPerToolFailureCap after 8 same-tool fails")
	}
	if !errors.Is(err, ErrPerToolFailureCap) {
		t.Fatalf("expected ErrPerToolFailureCap, got %v", err)
	}
	var pe *PerToolFailureError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PerToolFailureError via errors.As, got %T", err)
	}
	if pe.Tool != "bash" {
		t.Errorf("expected Tool=bash, got %q", pe.Tool)
	}
}
