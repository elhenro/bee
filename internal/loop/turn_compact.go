package loop

import (
	"context"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/session"
)

// Compact replaces the session's older messages with a summary in-place.
// Used by the /compact slash command. Persisting compacted history requires
// a session-file rewrite which is out of scope for F4 — the call returns
// success and the next turn loop's in-memory slice is re-evaluated by
// ShouldAutoCompact / Compact. Known limitation: replayed sessions on disk
// still contain the full history.
func (e *Engine) Compact(ctx context.Context) (CompactStats, error) {
	if e.Sessions == nil {
		return CompactStats{}, nil
	}
	msgs, err := session.Read(e.Sessions.ID())
	if err != nil {
		return CompactStats{}, err
	}
	out, stats, err := Compact(ctx, e.Provider, e.Cfg.DefaultModel, msgs)
	if err != nil {
		return stats, err
	}
	_ = out
	return stats, nil
}

// contextBudget returns the active model's real token window. Cache wins
// when populated (hardcoded table or live-learned via ProbeOllamaContext).
// For local providers the prewarm goroutine may not have answered yet on
// turn one — fall back to a 32k floor (matches ollama's default per-model
// allocation and avoids the bogus 14k-cap warnings the old SystemPromptBudget*4
// heuristic produced). Returns 0 for unknown remote models so callers treat
// it as "don't fire warnings".
func contextBudget(cfg config.Config) int {
	if cw := llm.ContextWindow(cfg.DefaultModel); cw > 0 {
		return cw
	}
	if config.IsLocalProvider(cfg.DefaultProvider) {
		return 32768
	}
	return 0
}

// scaledCompactThreshold widens the user-configured compaction threshold for
// large context windows. The fixed 0.75 default fires far too early on
// 128k-class models (sparse MoE: Qwen3.6-35B-A3B-4bit etc.) — at 96k tokens
// the agent still has 32k of breathing room, no reason to compact yet.
//
// Formula: derived = max(0.5, 1 - 8000/budget). Keeps at least 8000 tokens of
// headroom for the next turn's output regardless of window size. Only widens;
// never tightens the user's setting (so explicit low thresholds stay honored).
//
// budget<=0 or base<=0 returns base unchanged.
func scaledCompactThreshold(base float64, budget int) float64 {
	if budget <= 0 || base <= 0 {
		return base
	}
	derived := 1.0 - 8000.0/float64(budget)
	if derived < 0.5 {
		derived = 0.5
	}
	if derived > base {
		return derived
	}
	return base
}
