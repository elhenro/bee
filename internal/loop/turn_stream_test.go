package loop

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// textDeltaProvider emits two text deltas then done. No tool uses.
type textDeltaProvider struct{}

func (p *textDeltaProvider) Name() string { return "text-delta" }
func (p *textDeltaProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 4)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "hello "}
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "world"}
		ch <- llm.Event{Type: llm.EventDone}
	}()
	return ch, nil
}

func TestStreamCh_ReceivesDeltas(t *testing.T) {
	streamCh := make(chan string, 4)
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	eng := &Engine{
		Provider: &textDeltaProvider{},
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		StreamCh: streamCh,
		Cfg:      cfg,
		Cwd:      ".",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := eng.Run(ctx, "test"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(streamCh)
	var got strings.Builder
	for d := range streamCh {
		got.WriteString(d)
	}
	if got.String() != "hello world" {
		t.Errorf("want %q, got %q", "hello world", got.String())
	}
}
