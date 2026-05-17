package loop

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// reasoningThenTextProvider returns thinking-only on call 1, plain text on call 2.
// reproduces deepseek-v4-flash silent-stall: reasoning emitted, then stop with
// neither text nor tool_use. loop must nudge once and continue.
type reasoningThenTextProvider struct{ calls atomic.Int32 }

func (p *reasoningThenTextProvider) Name() string { return "reasoning-stall" }
func (p *reasoningThenTextProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	n := p.calls.Add(1)
	ch := make(chan llm.Event, 4)
	go func() {
		defer close(ch)
		switch n {
		case 1:
			ch <- llm.Event{Type: llm.EventThinkingDelta, Delta: "let me think..."}
			ch <- llm.Event{Type: llm.EventDone}
		default:
			ch <- llm.Event{Type: llm.EventTextDelta, Delta: "done"}
			ch <- llm.Event{Type: llm.EventDone}
		}
	}()
	return ch, nil
}

func TestRun_ReasoningOnlyTurn_Nudges(t *testing.T) {
	prov := &reasoningThenTextProvider{}
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Mode = "edit"
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	eng := &Engine{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Cfg:      cfg,
		Cwd:      ".",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := eng.Run(ctx, "test")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := prov.calls.Load(); got != 2 {
		t.Fatalf("provider call count: want 2 (nudge retry), got %d", got)
	}
	if !strings.Contains(res.FinalText, "done") {
		t.Errorf("final text after nudge: want contains 'done', got %q", res.FinalText)
	}
	var sawNudge bool
	for _, m := range res.Messages {
		if m.Role != types.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.Type == types.BlockText && strings.Contains(b.Text, "[nudge]") {
				sawNudge = true
			}
		}
	}
	if !sawNudge {
		t.Error("expected synthetic [nudge] user message in rollout")
	}
}

// reasoningOnlyAlwaysProvider always returns thinking-only. loop must nudge
// once and then terminate cleanly rather than nudging in a tight loop.
type reasoningOnlyAlwaysProvider struct{ calls atomic.Int32 }

func (p *reasoningOnlyAlwaysProvider) Name() string { return "reasoning-stall-perma" }
func (p *reasoningOnlyAlwaysProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	p.calls.Add(1)
	ch := make(chan llm.Event, 2)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventThinkingDelta, Delta: "still thinking..."}
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}

func TestRun_ReasoningOnlyTurn_NudgesAtMostOnce(t *testing.T) {
	prov := &reasoningOnlyAlwaysProvider{}
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Mode = "edit"
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	eng := &Engine{
		Provider: prov,
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Cfg:      cfg,
		Cwd:      ".",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := eng.Run(ctx, "test"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := prov.calls.Load(); got != 2 {
		t.Fatalf("provider call count: want 2 (initial + one nudge retry), got %d", got)
	}
}
