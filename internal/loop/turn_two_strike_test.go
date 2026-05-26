package loop

import (
	"context"
	"errors"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// scripted provider emits the same failing bash call twice. expect
// Engine.Run to return ErrTwoStrike (wrapped in TwoStrikeError).
func TestEngineRun_TwoStrikeOnSameFailingCall(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&stubTool{
		name: "bash",
		desc: "shell",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{}, errors.New("simulated bash failure")
		},
	})

	use := &types.ToolUse{ID: "u-same", Name: "bash", Input: map[string]any{"command": "boom"}}
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: use},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventToolUse, ToolUse: use},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
	}}
	eng, _ := newEngine(p, reg)

	_, err := eng.Run(context.Background(), "go")
	if err == nil {
		t.Fatalf("expected ErrTwoStrike, got nil")
	}
	if !errors.Is(err, ErrTwoStrike) {
		t.Fatalf("expected ErrTwoStrike, got %v", err)
	}
	var tse *TwoStrikeError
	if !errors.As(err, &tse) {
		t.Fatalf("expected TwoStrikeError via errors.As, got %T", err)
	}
	if tse.Use.Name != "bash" {
		t.Fatalf("expected wrapped Use.Name=bash, got %q", tse.Use.Name)
	}
}

// when the same call fails then succeeds then fails, two-strike must NOT
// fire — the streak is broken by the success in the middle.
func TestEngineRun_TwoStrikeBrokenBySuccess(t *testing.T) {
	calls := 0
	reg := tools.NewRegistry()
	reg.Register(&stubTool{
		name: "read",
		desc: "read",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			calls++
			if calls == 2 {
				return tools.Result{Content: "ok"}, nil
			}
			return tools.Result{}, errors.New("simulated read failure")
		},
	})

	use := &types.ToolUse{ID: "u-rd", Name: "read", Input: map[string]any{"path": "x"}}
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: use},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventToolUse, ToolUse: use},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventToolUse, ToolUse: use},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)

	_, err := eng.Run(context.Background(), "go")
	if errors.Is(err, ErrTwoStrike) {
		t.Fatalf("two-strike should not fire when success breaks the streak; got %v", err)
	}
}
