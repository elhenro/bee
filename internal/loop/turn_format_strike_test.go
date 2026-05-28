package loop

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// jsonToolAttemptTwiceThenSuccess: malformed envelope on turns 1+2, plain text on 3.
// previously bee fired one format nudge then exited silently on the 2nd slip
// (session 55a4e994). loop must nudge BOTH times and keep going.
type jsonToolAttemptTwiceThenSuccess struct{ calls atomic.Int32 }

func (p *jsonToolAttemptTwiceThenSuccess) Name() string { return "json-twice" }
func (p *jsonToolAttemptTwiceThenSuccess) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	n := p.calls.Add(1)
	ch := make(chan llm.Event, 4)
	go func() {
		defer close(ch)
		switch n {
		case 1, 2:
			ch <- llm.Event{Type: llm.EventTextDelta, Delta: `{"type":"shell","command":"git status"}`}
			ch <- llm.Event{Type: llm.EventDone}
		default:
			ch <- llm.Event{Type: llm.EventTextDelta, Delta: "done"}
			ch <- llm.Event{Type: llm.EventDone}
		}
	}()
	return ch, nil
}

// F3: format nudge must refire on a second malformed envelope so the loop
// doesn't exit silently after the first miss.
func TestRun_FormatSlipNudgesTwice(t *testing.T) {
	prov := &jsonToolAttemptTwiceThenSuccess{}
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Mode = "edit"
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "shell", desc: "run shell", fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
		return tools.Result{}, nil
	}})
	eng := &Engine{
		Provider: prov,
		Tools:    reg,
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
	if got := prov.calls.Load(); got != 3 {
		t.Fatalf("provider call count: want 3 (initial + 2 nudge retries), got %d", got)
	}
	nudges := 0
	for _, m := range res.Messages {
		if m.Role != types.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.Type == types.BlockText && strings.Contains(b.Text, "[nudge]") {
				nudges++
			}
		}
	}
	if nudges < 2 {
		t.Errorf("expected at least 2 format nudges, got %d", nudges)
	}
}

// F3: nudge must reference the real tool name the model tried to call so the
// model sees a concrete example (not a `tool_name` placeholder it then echoes).
func TestRun_FormatNudgeIncludesRealToolName(t *testing.T) {
	prov := &jsonToolAttemptTwiceThenSuccess{}
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Mode = "edit"
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "shell", desc: "x", fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
		return tools.Result{}, nil
	}})
	eng := &Engine{Provider: prov, Tools: reg, Memory: stubMemStore{}, Cfg: cfg, Cwd: "."}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, _ := eng.Run(ctx, "test")
	var nudgeBody string
	for _, m := range res.Messages {
		if m.Role != types.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if strings.Contains(b.Text, "[nudge]") {
				nudgeBody = b.Text
			}
		}
	}
	if !strings.Contains(nudgeBody, "<shell>") {
		t.Errorf("nudge must include concrete example `<shell>{...}</shell>`; got: %s", nudgeBody)
	}
}

// parenProseSlip: model emits markdown-summary prose like
// `(read)\n internal/config/config.go` instead of `<read>{...}</read>`.
// Observed with qwen3.6-A3B in textmode — model drifts from envelope shape to
// a parenthesised prose recap. Without paren detection, the slip detector
// missed and the loop returned silently. Now it should slip-strike + nudge.
type parenProseSlip struct{ calls atomic.Int32 }

func (p *parenProseSlip) Name() string { return "paren-prose" }
func (p *parenProseSlip) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	p.calls.Add(1)
	ch := make(chan llm.Event, 2)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "Let's check the config structure first.\n(read)\n internal/config/config.go"}
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}

func TestRun_FormatSlipDetectsParenProse(t *testing.T) {
	prov := &parenProseSlip{}
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Mode = "edit"
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "read", desc: "read file", fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
		return tools.Result{}, nil
	}})
	eng := &Engine{Provider: prov, Tools: reg, Memory: stubMemStore{}, Cfg: cfg, Cwd: "."}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, _ := eng.Run(ctx, "test")
	sawNudge := false
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
		t.Fatalf("expected format nudge after `(read)` paren-prose slip, got none (msgs=%d)", len(res.Messages))
	}
}

// jsonToolAttemptForever: always emits malformed envelope. previously bee
// nudged once then exited silently — model effectively wedged. F4: count 3
// consecutive format slips → bail with FormatStrikeError.
type jsonToolAttemptForever struct{ calls atomic.Int32 }

func (p *jsonToolAttemptForever) Name() string { return "json-forever" }
func (p *jsonToolAttemptForever) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	p.calls.Add(1)
	ch := make(chan llm.Event, 2)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: `{"type":"shell","command":"x"}`}
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}

// F4: 3 consecutive format slips → ErrFormatStrike bail.
func TestRun_FormatStrikeBailsAtThree(t *testing.T) {
	prov := &jsonToolAttemptForever{}
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Mode = "edit"
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	reg := tools.NewRegistry()
	_ = reg.Register(&stubTool{name: "shell", desc: "x", fn: func(_ context.Context, _ map[string]any) (tools.Result, error) {
		return tools.Result{}, nil
	}})
	eng := &Engine{Provider: prov, Tools: reg, Memory: stubMemStore{}, Cfg: cfg, Cwd: "."}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := eng.Run(ctx, "test")
	if err == nil {
		t.Fatalf("expected ErrFormatStrike after 3 consecutive format slips")
	}
	if !errors.Is(err, ErrFormatStrike) {
		t.Fatalf("expected ErrFormatStrike, got %v", err)
	}
	if got := prov.calls.Load(); got > 4 {
		t.Errorf("want at most 4 provider calls (3 slips then bail), got %d", got)
	}
}
