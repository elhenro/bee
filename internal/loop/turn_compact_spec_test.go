package loop

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// specProvider answers tool calls for `total` rounds, then text. A compaction
// request (recognized by its system prompt) gets a canned summary. It records
// how many main-model requests had completed when each compaction event happened.
type specProvider struct {
	mu            sync.Mutex
	total         int
	mainCalls     int
	compactCalls  int
	compactAt     int // main calls done when the summarizer was asked
	firstSeenAt   int // main calls done before the first request carrying the summary
	failSummarize bool
}

func (p *specProvider) Name() string { return "spec" }

func (p *specProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	isSummary := strings.HasPrefix(req.System, "You compress conversation history")
	if !isSummary {
		time.Sleep(3 * time.Millisecond) // model latency, so the summary goroutine gets scheduled
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ch := make(chan llm.Event, 4)
	defer close(ch)
	if isSummary {
		p.compactCalls++
		p.compactAt = p.mainCalls
		if p.failSummarize {
			ch <- llm.Event{Type: llm.EventError, Err: fmt.Errorf("summarizer down")}
			return ch, nil
		}
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "short summary"}
		ch <- llm.Event{Type: llm.EventDone}
		return ch, nil
	}
	if p.firstSeenAt == 0 && carriesSummary(req.Messages) {
		p.firstSeenAt = p.mainCalls
	}
	p.mainCalls++
	if p.mainCalls > p.total {
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "all done"}
		ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
		return ch, nil
	}
	ch <- llm.Event{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
		ID: fmt.Sprintf("u%d", p.mainCalls), Name: "bash", Input: map[string]any{"command": fmt.Sprintf("step %d", p.mainCalls)},
	}}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "tool_use"}
	return ch, nil
}

func carriesSummary(msgs []types.Message) bool {
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == types.BlockText && strings.HasPrefix(c.Text, "[compacted history]") {
				return true
			}
		}
	}
	return false
}

func specEngine(p *specProvider) *Engine {
	reg := tools.NewRegistry()
	n := 0
	_ = reg.Register(&stubTool{name: "bash", desc: "x", fn: func(context.Context, map[string]any) (tools.Result, error) {
		n++
		return tools.Result{Content: fmt.Sprintf("out %d %s", n, strings.Repeat("x", 6000))}, nil
	}})
	eng, _ := newEngine(p, reg)
	eng.Cfg.DefaultModel = "deepseek-reasoner" // known 65k window, so the budget is fixed
	return eng
}

// the summary is produced while the loop keeps going, then swapped in with no
// stall: at least one main request runs between "summarizer asked" and "summary
// first used". An inline compaction would have no such gap.
func TestAutoCompactRunsInBackground(t *testing.T) {
	p := &specProvider{total: 45}
	eng := specEngine(p)
	res, err := eng.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if p.compactCalls != 1 {
		t.Fatalf("summarizer calls = %d, want 1", p.compactCalls)
	}
	if p.firstSeenAt == 0 {
		t.Fatal("summary never reached the model")
	}
	if p.firstSeenAt <= p.compactAt {
		t.Errorf("summary used at main call %d, asked at %d: compaction ran inline", p.firstSeenAt, p.compactAt)
	}
	if res.FinalText != "all done" {
		t.Errorf("FinalText = %q", res.FinalText)
	}
	// messages from after the snapshot survive the swap, in order.
	last := ""
	for _, m := range res.Messages {
		for _, c := range m.Content {
			if c.Type == types.BlockToolUse && c.Use != nil {
				last = c.Use.ID
			}
		}
	}
	if last != "u45" {
		t.Errorf("last tool_use after swap = %q, want u45", last)
	}
}

// a failing summarizer falls back to the inline path and the Run still finishes.
func TestAutoCompactSpecFailureFallsBackInline(t *testing.T) {
	p := &specProvider{total: 60, failSummarize: true}
	eng := specEngine(p)
	if _, err := eng.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// the failed background try plus one inline retry, then the failure latches:
	// a summarizer that stays down must not cost a request per iteration.
	if p.compactCalls != 2 {
		t.Errorf("summarizer calls = %d, want 2", p.compactCalls)
	}
}
