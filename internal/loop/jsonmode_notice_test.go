package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// jsonModeStub wraps stubProvider with the "+jsonmode" name suffix the
// JSONModeProvider wrapper adds.
type jsonModeStub struct{ stubProvider }

func (p *jsonModeStub) Name() string { return "stub+jsonmode" }

func drainWarns(ch chan string) []string {
	var out []string
	for {
		select {
		case w := <-ch:
			out = append(out, w)
		default:
			return out
		}
	}
}

// the first parsed tool call on a jsonmode provider fires exactly one notice,
// across Runs within the same session.
func TestJSONModeNotice_OncePerSession(t *testing.T) {
	script := [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "1", Name: "bash", Input: map[string]any{"command": "true"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "2", Name: "bash", Input: map[string]any{"command": "true"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "done again"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "bash", desc: "run", fn: func(ctx context.Context, in map[string]any) (tools.Result, error) {
		return tools.Result{Content: "ok"}, nil
	}})
	eng, _ := newEngine(&jsonModeStub{stubProvider{scripts: script}}, reg)
	eng.WarnCh = make(chan string, 8)

	if _, err := eng.Run(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	notices := 0
	for _, w := range drainWarns(eng.WarnCh) {
		if strings.Contains(w, "json tool mode active") {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("first run: want 1 notice, got %d", notices)
	}

	if _, err := eng.Run(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	for _, w := range drainWarns(eng.WarnCh) {
		if strings.Contains(w, "json tool mode active") {
			t.Fatalf("second run repeated the notice: %q", w)
		}
	}
}

// a plain provider never fires the notice.
func TestJSONModeNotice_PlainProviderSilent(t *testing.T) {
	script := [][]llm.Event{
		{
			{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "1", Name: "bash", Input: map[string]any{"command": "true"}}},
			{Type: llm.EventDone, StopReason: "tool_use"},
		},
		{
			{Type: llm.EventTextDelta, Delta: "done"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}
	reg := tools.NewRegistry()
	reg.Register(&stubTool{name: "bash", desc: "run", fn: func(ctx context.Context, in map[string]any) (tools.Result, error) {
		return tools.Result{Content: "ok"}, nil
	}})
	eng, _ := newEngine(&stubProvider{scripts: script}, reg)
	eng.WarnCh = make(chan string, 8)

	if _, err := eng.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	for _, w := range drainWarns(eng.WarnCh) {
		if strings.Contains(w, "json tool mode active") {
			t.Fatalf("plain provider fired jsonmode notice: %q", w)
		}
	}
}
