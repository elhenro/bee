package loop

import (
	"context"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// probeContextWindow resolves the active model's context window for models the
// hardcoded table lacks. A local backend answers from localhost in ms, so that
// probe stays inline.
// A hosted lookup is a network round trip the first request must not wait on:
// it runs detached, and contextBudget reads the cache live so a later iteration
// picks the window up.
func (e *Engine) probeContextWindow(ctx context.Context) {
	name, model := e.Cfg.DefaultProvider, e.Cfg.DefaultModel
	pc, ok := e.Cfg.Providers[name]
	if !ok {
		return
	}
	if config.IsLocalProvider(name) {
		_ = llm.ProbeContextLength(ctx, name, pc, model)
		return
	}
	if llm.ContextWindow(model) > 0 {
		return
	}
	// detached from the turn: a canceled turn must not leave the model marked
	// probed with nothing cached.
	go llm.ProbeContextLength(context.WithoutCancel(ctx), name, pc, model)
}
