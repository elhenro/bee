package loop

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// usageProvider emits one text delta then a done event carrying the supplied
// token counts and optional provider-reported cost.
type usageProvider struct {
	in, out int
	usd     float64
}

func (p *usageProvider) Name() string { return "uprov" }
func (p *usageProvider) Stream(_ context.Context, _ llm.Request) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 3)
	go func() {
		defer close(ch)
		ch <- llm.Event{Type: llm.EventTextDelta, Delta: "ok"}
		ch <- llm.Event{Type: llm.EventDone, Usage: &llm.Usage{InputTokens: p.in, OutputTokens: p.out, CostUSD: p.usd}}
	}()
	return ch, nil
}

func runOneUsageTurn(t *testing.T, p *usageProvider) cost.UsageRecord {
	t.Helper()
	t.Setenv("BEE_USAGE_LOG", filepath.Join(t.TempDir(), "usage.jsonl"))
	t.Setenv("BEE_LIFETIME_TOKENS", filepath.Join(t.TempDir(), "life.json"))
	cost.ResetUsageForTest()
	cost.ResetLifetimeForTest()

	cfg := config.Defaults()
	cfg.Sandbox = config.SandboxConfig{Scope: "danger-full-access", Approval: "never"}
	eng := &Engine{
		Provider: p,
		Tools:    tools.NewRegistry(),
		Memory:   stubMemStore{},
		Costs:    cost.New(),
		Cfg:      cfg,
		Cwd:      ".",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := eng.Run(ctx, "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	recs, err := cost.ReadUsage()
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 usage record, got %d", len(recs))
	}
	return recs[0]
}

func TestTurnLogsProviderReportedCost(t *testing.T) {
	r := runOneUsageTurn(t, &usageProvider{in: 1234, out: 567, usd: 0.0042})
	if r.Input != 1234 || r.Output != 567 {
		t.Errorf("tokens = %d/%d, want 1234/567", r.Input, r.Output)
	}
	if !r.CostReported || r.USD != 0.0042 {
		t.Errorf("want provider-reported 0.0042, got reported=%v usd=%v", r.CostReported, r.USD)
	}
	if r.Provider != "openrouter" || r.Model != cfgDefaultModel() {
		t.Errorf("provider/model = %q/%q", r.Provider, r.Model)
	}
}

func TestTurnLogsEstimatedCost(t *testing.T) {
	// no provider cost → fall back to the static price table. default model is
	// deepseek-v4-flash @ 0.07/M in + 1.10/M out; 1M each → $1.17.
	r := runOneUsageTurn(t, &usageProvider{in: 1_000_000, out: 1_000_000, usd: 0})
	if r.CostReported {
		t.Error("cost should be flagged as estimated when provider reports none")
	}
	if math.Abs(r.USD-1.17) > 1e-9 {
		t.Errorf("estimated cost = %v, want 1.17", r.USD)
	}
}

func cfgDefaultModel() string { return config.Defaults().DefaultModel }
