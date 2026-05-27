package loop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// 2nd identical failing call must NOT bail — only inject a corrective nudge
// into the tool_result so the model can self-correct on the next iter.
// previously bee bailed at the 2nd, giving the model no chance to recover from
// a thin error message (e.g. "path escapes workspace root" with no echo).
func TestEngineRun_TwoStrikeInjectsNudgeNoBail(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "bash",
		desc: "shell",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{}, errors.New("simulated bash failure")
		},
	})
	use := &types.ToolUse{ID: "u-same", Name: "bash", Input: map[string]any{"command": "boom"}}
	p := &stubProvider{scripts: [][]llm.Event{
		// 1st fail
		{{Type: llm.EventToolUse, ToolUse: use}, {Type: llm.EventDone, StopReason: "tool_use"}},
		// 2nd identical fail → nudge injected, NO bail
		{{Type: llm.EventToolUse, ToolUse: use}, {Type: llm.EventDone, StopReason: "tool_use"}},
		// model corrects course
		{{Type: llm.EventTextDelta, Delta: "ok"}, {Type: llm.EventDone, StopReason: "stop"}},
	}}
	eng, _ := newEngine(p, reg)

	res, err := eng.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("loop must continue past 2nd identical fail; got: %v", err)
	}
	if errors.Is(err, ErrTwoStrike) {
		t.Fatalf("must not bail on 2nd identical fail")
	}
	// confirm a `[two-strike]` nudge landed in some tool_result content.
	var nudgeSeen bool
	for _, m := range res.Messages {
		for _, c := range m.Content {
			if c.Result != nil && strings.Contains(c.Result.Content, "[two-strike]") {
				nudgeSeen = true
			}
		}
	}
	if !nudgeSeen {
		t.Errorf("expected [two-strike] nudge prefix in a tool_result; messages=%+v", res.Messages)
	}
}

// 5 identical failing calls in a row → ErrTwoStrike with Class populated.
func TestEngineRun_TwoStrikeHardBailAtFive(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{
		name: "write",
		desc: "write",
		fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
			return tools.Result{Content: `path "/tmp/x" escapes workspace root "/repo"`, IsError: true}, nil
		},
	})
	use := &types.ToolUse{ID: "u-w", Name: "write", Input: map[string]any{"path": "/tmp/x", "content": "x"}}
	script := []llm.Event{
		{Type: llm.EventToolUse, ToolUse: use},
		{Type: llm.EventDone, StopReason: "tool_use"},
	}
	p := &stubProvider{scripts: [][]llm.Event{script, script, script, script, script}}
	eng, _ := newEngine(p, reg)

	_, err := eng.Run(context.Background(), "go")
	if err == nil {
		t.Fatalf("expected ErrTwoStrike after 5 identical fails")
	}
	if !errors.Is(err, ErrTwoStrike) {
		t.Fatalf("expected ErrTwoStrike, got %v", err)
	}
	var tse *TwoStrikeError
	if !errors.As(err, &tse) {
		t.Fatalf("expected TwoStrikeError via errors.As, got %T", err)
	}
	if tse.Use.Name != "write" {
		t.Fatalf("expected wrapped Use.Name=write, got %q", tse.Use.Name)
	}
	if tse.Class == "" {
		t.Errorf("expected Class to be populated (regression: empty class= in user transcript)")
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
