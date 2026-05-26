package loop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/escalate"
	"github.com/elhenro/bee/internal/types"
)

// when the model invokes escalate the engine must:
//   1. record a tool_result block in the transcript with the reason
//   2. return ErrEscalate (typed EscalateError) from Run
//   3. NOT keep iterating after the escalation
func TestEngineRun_EscalateToolBailsLoop(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(escalate.New())

	use := &types.ToolUse{
		ID:    "u-esc",
		Name:  "escalate",
		Input: map[string]any{"reason": "stuck on schema", "suggested_next_action": "ask the user"},
	}
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: use},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		// would be a 2nd round if escalate didn't bail — should never run.
		{
			{Type: llm.EventTextDelta, Delta: "should-not-appear"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, reg)

	res, err := eng.Run(context.Background(), "do something")
	if err == nil {
		t.Fatalf("expected ErrEscalate, got nil")
	}
	if !errors.Is(err, ErrEscalate) {
		t.Fatalf("expected ErrEscalate, got %v", err)
	}
	var ee *EscalateError
	if !errors.As(err, &ee) {
		t.Fatalf("expected EscalateError via errors.As, got %T", err)
	}
	if ee.Reason != "stuck on schema" {
		t.Errorf("wrong reason: %q", ee.Reason)
	}
	if p.calls.Load() != 1 {
		t.Errorf("provider called %d times; escalate must bail before 2nd round", p.calls.Load())
	}
	// transcript must record what happened.
	foundEscalateBlock := false
	for _, m := range res.Messages {
		if m.Role != types.RoleTool {
			continue
		}
		for _, c := range m.Content {
			if c.Type == types.BlockToolResult && c.Result != nil &&
				strings.Contains(c.Result.Content, "[escalate]") {
				foundEscalateBlock = true
			}
		}
	}
	if !foundEscalateBlock {
		t.Errorf("expected [escalate] block in transcript")
	}
}
