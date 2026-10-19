package loop

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

type timedEvent struct {
	after time.Duration // wait before emitting
	ev    llm.Event
}

// timedProvider plays one timed script, then a text answer.
type timedProvider struct {
	mu    sync.Mutex
	calls int
	turn  []timedEvent
	doneT time.Time // when the first turn's Done event went out
}

func (p *timedProvider) Name() string { return "timed" }

func (p *timedProvider) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	p.calls++
	first := p.calls == 1
	p.mu.Unlock()
	ch := make(chan llm.Event, 8)
	go func() {
		defer close(ch)
		if !first {
			ch <- llm.Event{Type: llm.EventTextDelta, Delta: "fin"}
			ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
			return
		}
		for _, te := range p.turn {
			select {
			case <-time.After(te.after):
			case <-ctx.Done():
				return
			}
			if te.ev.Type == llm.EventDone {
				p.mu.Lock()
				p.doneT = time.Now()
				p.mu.Unlock()
			}
			ch <- te.ev
		}
	}()
	return ch, nil
}

func toolUse(id, name string, in map[string]any) llm.Event {
	return llm.Event{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: id, Name: name, Input: in}}
}

type stamps struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (s *stamps) mark(k string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]time.Time{}
	}
	s.m[k] = time.Now()
}

func (s *stamps) at(k string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[k]
}

// a slow read emitted first runs while the model writes its sibling, so it adds
// nothing after the stream ends.
func TestEarlyDispatchOverlapsReadWithStream(t *testing.T) {
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "read", desc: "x", fn: func(_ context.Context, in map[string]any) (tools.Result, error) {
		if in["path"] == "slow" {
			time.Sleep(100 * time.Millisecond)
		}
		return tools.Result{Content: fmt.Sprint(in["path"])}, nil
	}})
	p := &timedProvider{turn: []timedEvent{
		{0, toolUse("a", "read", map[string]any{"path": "slow"})},
		{120 * time.Millisecond, toolUse("b", "read", map[string]any{"path": "fast"})},
		{0, llm.Event{Type: llm.EventDone, StopReason: "tool_use"}},
	}}
	eng, _ := newEngine(p, reg)
	start := time.Now()
	res, err := eng.Run(context.Background(), "go")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// stream ends at ~120ms; the slow read must already be done, so the whole
	// Run is ~120ms, not 120 + 100.
	if d := time.Since(start); d > 190*time.Millisecond {
		t.Errorf("Run took %v: the slow read did not overlap the stream", d)
	}
	// results keep call order and ids.
	var got []string
	for _, m := range res.Messages {
		for _, c := range m.Content {
			if c.Type == types.BlockToolResult && c.Result != nil {
				got = append(got, c.Result.UseID+"="+c.Result.Content)
			}
		}
	}
	if fmt.Sprint(got) != "[a=slow b=fast]" {
		t.Errorf("tool results = %v, want [a=slow b=fast]", got)
	}
}

// early dispatch must not reorder effects: a mutator waits for the stream to end,
// and a read emitted after it waits for the mutator.
func TestEarlyDispatchKeepsMutatorBarrier(t *testing.T) {
	var st stamps
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "read", desc: "x", fn: func(_ context.Context, in map[string]any) (tools.Result, error) {
		st.mark("read:" + fmt.Sprint(in["path"]))
		return tools.Result{Content: "r"}, nil
	}})
	_ = reg.Register(&stubTool{name: "bash", desc: "x", fn: func(context.Context, map[string]any) (tools.Result, error) {
		st.mark("bash:start")
		time.Sleep(30 * time.Millisecond)
		st.mark("bash:end")
		return tools.Result{Content: "b"}, nil
	}})
	p := &timedProvider{turn: []timedEvent{
		{0, toolUse("a", "read", map[string]any{"path": "before"})},
		{0, toolUse("b", "bash", map[string]any{"command": "x"})},
		{0, toolUse("c", "read", map[string]any{"path": "after"})},
		{80 * time.Millisecond, llm.Event{Type: llm.EventDone, StopReason: "tool_use"}},
	}}
	eng, _ := newEngine(p, reg)
	if _, err := eng.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	p.mu.Lock()
	doneT := p.doneT
	p.mu.Unlock()
	if !st.at("read:before").Before(doneT) {
		t.Error("leading read did not start before the stream ended")
	}
	if st.at("bash:start").Before(doneT) {
		t.Error("mutator started before the stream ended")
	}
	if st.at("read:after").Before(st.at("bash:end")) {
		t.Error("read emitted after a mutator ran before the mutator finished")
	}
}
