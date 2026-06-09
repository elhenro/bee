package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

func sayOnlyEngine(t *testing.T, scripts [][]llm.Event) *Engine {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "bash", desc: "run", fn: func(ctx context.Context, in map[string]any) (tools.Result, error) {
		return tools.Result{Content: "ok"}, nil
	}})
	eng, _ := newEngine(&jsonModeStub{stubProvider{scripts: scripts}}, reg)
	return eng
}

func countNudges(msgs []types.Message, marker string) int {
	n := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == types.BlockText && strings.Contains(b.Text, marker) {
				n++
			}
		}
	}
	return n
}

// an actionable request answered with a say-only first turn gets one nudge;
// the model then acts and the tool runs.
func TestSayOnlyStopNudge_FiresOnceThenActs(t *testing.T) {
	eng := sayOnlyEngine(t, [][]llm.Event{
		{
			{Type: llm.EventTextDelta, Delta: "add bush objects to world gen"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "1", Name: "bash", Input: map[string]any{"command": "ls"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "bushes added, done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	})
	res, err := eng.Run(context.Background(), "add bushes to the game world generation")
	if err != nil {
		t.Fatal(err)
	}
	if got := countNudges(res.Messages, "no tool call but the request needs work"); got != 1 {
		t.Fatalf("want exactly 1 say-only nudge, got %d", got)
	}
	if res.FinalText != "bushes added, done" {
		t.Fatalf("model did not continue after nudge: %q", res.FinalText)
	}
}

// a repeat say-only turn after the nudge is accepted (no infinite nudging).
func TestSayOnlyStopNudge_SecondSayAccepted(t *testing.T) {
	eng := sayOnlyEngine(t, [][]llm.Event{
		{
			{Type: llm.EventTextDelta, Delta: "what kind of bushes?"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "need bush type first: decorative or collision?"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	})
	res, err := eng.Run(context.Background(), "add bushes to the world")
	if err != nil {
		t.Fatal(err)
	}
	if got := countNudges(res.Messages, "no tool call but the request needs work"); got != 1 {
		t.Fatalf("want exactly 1 nudge, got %d", got)
	}
	if res.FinalText != "need bush type first: decorative or collision?" {
		t.Fatalf("second say not accepted as answer: %q", res.FinalText)
	}
}

// chat turns never trigger the guard.
func TestSayOnlyStopNudge_ChatSilent(t *testing.T) {
	eng := sayOnlyEngine(t, [][]llm.Event{
		{
			{Type: llm.EventTextDelta, Delta: "hello! what do you want to work on?"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	})
	res, err := eng.Run(context.Background(), "hi")
	if err != nil {
		t.Fatal(err)
	}
	if got := countNudges(res.Messages, "no tool call but the request needs work"); got != 0 {
		t.Fatalf("chat turn nudged: %d", got)
	}
}

// non-jsonmode providers keep the old behavior.
func TestSayOnlyStopNudge_PlainProviderSilent(t *testing.T) {
	reg := tools.NewRegistry()
	eng, _ := newEngine(&stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventTextDelta, Delta: "ok"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}, reg)
	res, err := eng.Run(context.Background(), "add a bush")
	if err != nil {
		t.Fatal(err)
	}
	if got := countNudges(res.Messages, "no tool call but the request needs work"); got != 0 {
		t.Fatalf("plain provider nudged: %d", got)
	}
}
