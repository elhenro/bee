package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// stepProvider plays one scripted step per request and records what it saw.
// step receives the 1-based request number.
type stepProvider struct {
	mu   sync.Mutex
	reqs []llm.Request
	step func(n int) []llm.Event
}

func (p *stepProvider) Name() string { return "stub" }
func (p *stepProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	n := len(p.reqs)
	p.mu.Unlock()
	events := p.step(n)
	ch := make(chan llm.Event, len(events)+1)
	for _, e := range events {
		ch <- e
	}
	ch <- llm.Event{Type: llm.EventDone, StopReason: "stop"}
	close(ch)
	return ch, nil
}

func textEvent(s string) llm.Event { return llm.Event{Type: llm.EventTextDelta, Delta: s} }

func messageTexts(msgs []types.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		for _, c := range m.Content {
			b.WriteString(c.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func bgTestEngine(prov llm.Provider, mutate func(*config.Config)) *loop.Engine {
	cfg := config.Defaults()
	cfg.Memory.Enabled = false
	if mutate != nil {
		mutate(&cfg)
	}
	return &loop.Engine{Provider: prov, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: ".", Stdout: io.Discard}
}

// a follow-up from the inbox runs with the earlier turns in context, not blind.
func TestBgLoopFollowUpCarriesHistory(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("BEE_AGENT_WORKTREE", "")
	const id = "carry-history"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prov := &stepProvider{step: func(n int) []llm.Event {
		if n == 1 {
			_ = bgreg.InboxAppend(id, "second ask")
			return []llm.Event{textEvent("first answer")}
		}
		cancel()
		return []llm.Event{textEvent("second answer")}
	}}
	if err := runBgLoop(ctx, bgTestEngine(prov, nil), id, "first ask"); err != nil {
		t.Fatalf("runBgLoop: %v", err)
	}
	if len(prov.reqs) != 2 {
		t.Fatalf("want 2 requests, got %d", len(prov.reqs))
	}
	seen := messageTexts(prov.reqs[1].Messages)
	for _, want := range []string{"first ask", "first answer", "second ask"} {
		if !strings.Contains(seen, want) {
			t.Errorf("second turn lost %q from context:\n%s", want, seen)
		}
	}
}

// a turn that stops on a recoverable condition is resumed like `bee run`
// does, instead of marking the agent failed for good.
func TestBgLoopResumesRecoverableStop(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("BEE_AGENT_WORKTREE", "")
	const id = "resume-recover"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// request 1 burns the single allowed iteration on a tool call, request 2 is
	// the loop's tool-less wind-down, request 3 is the auto-resumed turn.
	prov := &stepProvider{step: func(n int) []llm.Event {
		switch n {
		case 1:
			return []llm.Event{{Type: llm.EventToolUse, ToolUse: &types.ToolUse{ID: "t1", Name: "missing", Input: map[string]any{}}}}
		case 2:
			return []llm.Event{textEvent("wound down")}
		}
		cancel()
		return []llm.Event{textEvent("recovered")}
	}}
	eng := bgTestEngine(prov, func(c *config.Config) { c.MaxIterations = 1 })
	if err := runBgLoop(ctx, eng, id, "long task"); err != nil {
		t.Fatalf("runBgLoop: %v", err)
	}
	if len(prov.reqs) < 3 {
		t.Fatalf("turn was not resumed after max-iterations, got %d request(s)", len(prov.reqs))
	}
	if st, err := bgreg.Read(id); err != nil || st.State == bgreg.StateFailed {
		t.Fatalf("agent marked failed after a recoverable stop: %+v, %v", st, err)
	}
}
