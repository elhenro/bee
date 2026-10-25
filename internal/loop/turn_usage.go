package loop

import (
	"github.com/elhenro/bee/internal/cost"
	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/llm"
)

// recordDone books a finished request: the done event, spend, lifetime totals
// and the Run's token counters.
func (e *Engine) recordDone(model string, u *llm.Usage) {
	if e.JSONEmitter != nil {
		ju := &jsonmode.Usage{}
		if u != nil {
			ju.Input, ju.Output = u.InputTokens, u.OutputTokens
		}
		e.JSONEmitter.Emit(jsonmode.Event{Type: "done", Usage: ju})
	}
	if u == nil {
		return
	}
	if e.Costs != nil {
		// prefer provider-reported spend over the static estimate.
		cev := e.Costs.RecordReported(e.Cfg.DefaultProvider, model, u.InputTokens, u.OutputTokens, u.CostUSD)
		cost.AppendUsage(cost.UsageRecord{
			Time:         cev.Time,
			Provider:     e.Cfg.DefaultProvider,
			Model:        model,
			Input:        u.InputTokens,
			Output:       u.OutputTokens,
			Cached:       u.CachedTokens,
			USD:          cev.USD,
			CostReported: u.CostUSD > 0,
		})
	}
	// Bump persisted lifetime totals so the splash banner can show
	// "1.2M tok" across all bee sessions ever. Separate from the
	// process-local Costs tracker (which resets on /new), and
	// hooked here so unit tests using cost.New() directly stay
	// hermetic — production has exactly one place tokens enter.
	cost.AddLifetime(u.InputTokens, u.OutputTokens)
	if u.InputTokens > 0 {
		e.run.lastInputTokens = u.InputTokens
	}
	// cumulative spend feeds the adaptive token-budget cap in
	// the run loop. tracked separately from lastInputTokens (which is
	// a per-request snapshot, not a sum).
	e.run.cumInputTokens += u.InputTokens
	e.run.cumOutputTokens += u.OutputTokens
}
