package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
)

// recordingProvider answers work turns with distinct text and the judge with
// UNMET, keeping every request for inspection.
type recordingProvider struct {
	mu   sync.Mutex
	reqs []llm.Request
}

func (p *recordingProvider) Name() string { return "stub" }
func (p *recordingProvider) Stream(_ context.Context, req llm.Request) (<-chan llm.Event, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	n := len(p.reqs)
	p.mu.Unlock()
	ch := make(chan llm.Event, 2)
	if strings.Contains(req.System, "completion judge") {
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "VERDICT: UNMET - nothing"}
	} else {
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "did step " + strings.Repeat("x", n)}
	}
	ch <- llm.Event{Type: llm.EventDone}
	close(ch)
	return ch, nil
}

// every continuation turn must see the earlier turns, not just the
// "[goal] not satisfied" text.
func TestRunGoalHeadlessCarriesHistory(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	cfg.Memory.Enabled = false
	p := &recordingProvider{}
	eng := &loop.Engine{Provider: p, Tools: tools.NewRegistry(), Cfg: cfg, Cwd: "."}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	runGoalHeadless(ctx, eng, cfg, "sid", "make the build green")

	var work []int
	for _, r := range p.reqs {
		if !strings.Contains(r.System, "completion judge") {
			work = append(work, len(r.Messages))
		}
	}
	if len(work) < 2 {
		t.Fatalf("want at least 2 work turns, got %v", work)
	}
	for i := 1; i < len(work); i++ {
		if work[i] <= work[i-1] {
			t.Fatalf("work turn %d saw %d messages, previous saw %d: history not carried (%v)", i, work[i], work[i-1], work)
		}
	}
}
