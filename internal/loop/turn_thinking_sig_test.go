package loop

import (
	"context"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

type signedThinkingProvider struct{}

func (signedThinkingProvider) Name() string { return "signed" }
func (signedThinkingProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 4)
	ch <- llm.Event{Type: llm.EventThinkingDelta, Delta: "plan"}
	ch <- llm.Event{Type: llm.EventThinkingDelta, Signature: "sig-1"}
	ch <- llm.Event{Type: llm.EventTextDelta, Delta: "done"}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

func TestRun_ThinkingSignatureLandsOnBlock(t *testing.T) {
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Role = "worker"
	cfg.Compaction = config.CompactionConfig{Enabled: false}
	eng := &Engine{Provider: signedThinkingProvider{}, Tools: tools.NewRegistry(), Memory: stubMemStore{}, Cfg: cfg, Cwd: "."}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := eng.Run(ctx, "test")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, m := range res.Messages {
		for _, b := range m.Content {
			if b.Type == types.BlockThinking {
				if b.Text != "plan" || b.Signature != "sig-1" {
					t.Errorf("thinking block = %+v", b)
				}
				return
			}
		}
	}
	t.Fatal("no thinking block in transcript")
}
