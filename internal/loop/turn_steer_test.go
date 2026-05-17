package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// twoTurnProvider emits a tool_use on the first stream and a terminal text
// response on every subsequent call. Lets us observe steer injection that
// happens at the top of the second iteration.
type twoTurnProvider struct {
	turn int
}

func (p *twoTurnProvider) Name() string { return "two-turn" }

func (p *twoTurnProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 4)
	p.turn++
	t := p.turn
	go func() {
		defer close(ch)
		if t == 1 {
			ch <- llm.Event{Type: llm.EventToolUse, ToolUse: &types.ToolUse{
				ID: "tu1", Name: "echo", Input: map[string]any{},
			}}
		} else {
			ch <- llm.Event{Type: llm.EventTextDelta, Delta: "done"}
		}
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}

func TestSteerCh_InjectsBetweenIterations(t *testing.T) {
	steerCh := make(chan string, 1)
	steerCh <- "look at file X"

	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}

	eng := &Engine{
		Provider: &twoTurnProvider{},
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		SteerCh:  steerCh,
		Cfg:      cfg,
		Cwd:      ".",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := eng.Run(ctx, "initial")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, m := range res.Messages {
		if m.Role != types.RoleUser {
			continue
		}
		for _, c := range m.Content {
			if c.Type == types.BlockText && strings.Contains(c.Text, "[steer] look at file X") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("steer message not injected; messages=%+v", res.Messages)
	}
}

func TestSteerCh_NilDoesNotBlock(t *testing.T) {
	// Sanity: engine with nil SteerCh runs as before.
	p := &stubProvider{scripts: [][]llm.Event{
		{
			{Type: llm.EventTextDelta, Delta: "ok"},
			{Type: llm.EventDone, StopReason: "stop"},
		},
	}}
	eng, _ := newEngine(p, tools.NewRegistry())
	if _, err := eng.Run(context.Background(), "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestSteerCh_EmptyDrainNoOp(t *testing.T) {
	// Empty channel + tool_use loop should not inject anything.
	steerCh := make(chan string, 1) // empty
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}

	eng := &Engine{
		Provider: &twoTurnProvider{},
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		SteerCh:  steerCh,
		Cfg:      cfg,
		Cwd:      ".",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := eng.Run(ctx, "initial")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, m := range res.Messages {
		for _, c := range m.Content {
			if c.Type == types.BlockText && strings.Contains(c.Text, "[steer]") {
				t.Errorf("unexpected steer message present: %q", c.Text)
			}
		}
	}
}
