package loop

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

func okTool(name string) *stubTool {
	return &stubTool{name: name, desc: name, fn: func(context.Context, map[string]any) (tools.Result, error) {
		return tools.Result{Content: name + " ran"}, nil
	}}
}

func toolResultIDs(m types.Message) map[string]types.ToolResult {
	out := map[string]types.ToolResult{}
	for _, c := range m.Content {
		if c.Type == types.BlockToolResult && c.Result != nil {
			out[c.Result.UseID] = *c.Result
		}
	}
	return out
}

// a done signal alongside a tool call must not strand the tool_use.
func TestRun_DoneSignalWithToolCallStillDispatches(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(okTool("write"))
	p := &stubProvider{scripts: [][]llm.Event{{
		{Type: llm.EventTextDelta, Delta: "committing now\n" + doneSignalToken},
		{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "u1", Name: "write", Input: map[string]any{}}},
		{Type: llm.EventDone, StopReason: "tool_use"},
	}}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(context.Background(), "do it")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if p.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want 1 (done honored after dispatch)", p.calls.Load())
	}
	last := res.Messages[len(res.Messages)-1]
	if last.Role != types.RoleTool {
		t.Fatalf("last message role = %s, want tool result", last.Role)
	}
	if r, ok := toolResultIDs(last)["u1"]; !ok || r.IsError {
		t.Fatalf("tool u1 not executed cleanly: %+v", toolResultIDs(last))
	}
}

// ctx cancel mid-batch: finished tools keep their results, the rest get a
// synthetic canceled result, and the slice answers every tool_use.
func TestDispatchTools_CancelFillsEveryResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "write", desc: "w", fn: func(context.Context, map[string]any) (tools.Result, error) {
		cancel()
		return tools.Result{Content: "wrote"}, nil
	}})
	_ = reg.Register(okTool("edit"))
	eng, _ := newEngine(&stubProvider{}, reg)
	eng.run = eng.freshRunState()
	eng.run.allowedTools = nil // nil = no surface gate
	uses := []types.ToolUse{
		{ID: "a", Name: "write", Input: map[string]any{}},
		{ID: "b", Name: "edit", Input: map[string]any{}},
	}
	results, err := eng.dispatchTools(ctx, uses)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(results) != 2 || results[0].UseID != "a" || results[1].UseID != "b" {
		t.Fatalf("results do not line up with uses: %+v", results)
	}
	if results[0].Content != "wrote" || results[0].IsError {
		t.Errorf("finished tool lost its result: %+v", results[0])
	}
	if !results[1].IsError || !strings.Contains(results[1].Content, "canceled") {
		t.Errorf("unstarted tool not marked canceled: %+v", results[1])
	}
}

// the Run-level path must persist that tool message, not drop it.
func TestRun_CancelMidBatchKeepsToolMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "write", desc: "w", fn: func(context.Context, map[string]any) (tools.Result, error) {
		cancel()
		return tools.Result{Content: "wrote"}, nil
	}})
	_ = reg.Register(okTool("edit"))
	p := &stubProvider{scripts: [][]llm.Event{{
		{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "a", Name: "write", Input: map[string]any{}}},
		{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "b", Name: "edit", Input: map[string]any{}}},
		{Type: llm.EventDone, StopReason: "tool_use"},
	}}}
	eng, _ := newEngine(p, reg)
	res, err := eng.Run(ctx, "go")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	last := res.Messages[len(res.Messages)-1]
	got := toolResultIDs(last)
	if last.Role != types.RoleTool || len(got) != 2 {
		t.Fatalf("want a tool message answering both tool_use ids, got role=%s results=%v", last.Role, got)
	}
}

func TestShouldAutoCompactWithUsage_CountsContentPastReportedUsage(t *testing.T) {
	big := types.Message{Role: types.RoleTool, Content: []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "1", Content: strings.Repeat("x", 40_000)}},
	}}
	msgs := []types.Message{mkMsg(types.RoleUser, "hi"), big}
	// reported usage predates the 10k-token tool output.
	if !ShouldAutoCompactWithUsage("sys", msgs, 100, 1000, 0.8) {
		t.Error("want compaction when appended tool output alone exceeds the budget")
	}
}

type emptyStreamProvider struct{}

func (emptyStreamProvider) Name() string { return "stub" }
func (emptyStreamProvider) Stream(context.Context, llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 2)
	ch <- llm.Event{Type: llm.EventThinkingDelta, Delta: "hmm"}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

func TestCompact_EmptySummaryIsAnError(t *testing.T) {
	var msgs []types.Message
	for range 8 {
		msgs = append(msgs, mkMsg(types.RoleUser, "m"))
	}
	out, _, err := Compact(context.Background(), emptyStreamProvider{}, "m", msgs)
	if err == nil {
		t.Fatal("want error for empty summary")
	}
	if len(out) != len(msgs) {
		t.Errorf("history must be returned intact, got %d msgs", len(out))
	}
}

func TestCompact_DropsEphemeralNotices(t *testing.T) {
	var msgs []types.Message
	for range 8 {
		msgs = append(msgs, mkMsg(types.RoleUser, "m"))
	}
	note := mkMsg(types.RoleAssistant, "(auto-compacted)")
	note.ID, note.Ephemeral = "note", true
	msgs = append(msgs[:5], append([]types.Message{note}, msgs[5:]...)...)
	e := &Engine{Provider: &compactStubProvider{summary: "S"}}
	out, _, err := e.compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range out {
		if m.Ephemeral {
			t.Fatal("ephemeral notice survived compaction")
		}
	}
}

type captureProvider struct {
	mu   sync.Mutex
	reqs []llm.Request
	fail bool
}

func (c *captureProvider) Name() string { return "stub" }
func (c *captureProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	c.mu.Lock()
	c.reqs = append(c.reqs, req)
	c.mu.Unlock()
	ch := make(chan llm.Event, 2)
	if c.fail {
		ch <- llm.Event{Type: llm.EventError, Err: errors.New("boom")}
	} else {
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "summary"}
		ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	}
	close(ch)
	return ch, nil
}

func capHistory() *RunResult {
	use := &types.ToolUse{ID: "u", Name: "read", Input: map[string]any{"path": "a"}}
	return &RunResult{Messages: []types.Message{
		mkMsg(types.RoleUser, "task"),
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockToolUse, Use: use}}},
		{Role: types.RoleTool, Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "u", Content: "file"}}}},
	}}
}

func TestFinalSummaryOnCap_FlattensToolBlocks(t *testing.T) {
	p := &captureProvider{}
	eng, _ := newEngine(p, tools.NewRegistry())
	eng.run = eng.freshRunState()
	res := capHistory()
	eng.finalSummaryOnCap(context.Background(), res, "sys", "")
	if res.FinalText != "summary" {
		t.Fatalf("final text = %q", res.FinalText)
	}
	for _, m := range p.reqs[0].Messages {
		for _, c := range m.Content {
			if c.Type == types.BlockToolUse || c.Type == types.BlockToolResult {
				t.Fatalf("tool block leaked into a tool-less request: %+v", m)
			}
		}
	}
	if len(p.reqs[0].Tools) != 0 {
		t.Error("wind-down request must carry no tools")
	}
}

func TestFinalSummaryOnCap_FailureLeavesNoPrompt(t *testing.T) {
	eng, _ := newEngine(&captureProvider{fail: true}, tools.NewRegistry())
	eng.run = eng.freshRunState()
	res := capHistory()
	n := len(res.Messages)
	eng.finalSummaryOnCap(context.Background(), res, "sys", "")
	if len(res.Messages) != n {
		t.Errorf("failed wind-down appended %d messages", len(res.Messages)-n)
	}
}
