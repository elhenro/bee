package loop

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// Compact summarizes the session's older messages and returns the compacted
// slice plus stats. Caller is responsible for replacing the in-memory message
// list (e.g. TUI scrollback / InitialMessages) so the next turn sees the
// shorter history. The raw on-disk log stays append-only, but a checkpoint
// marker is appended so resume (session.ReadResume) reconstructs this shortened
// view instead of replaying the full history.
func (e *Engine) Compact(ctx context.Context) ([]types.Message, CompactStats, error) {
	if e.Sessions == nil {
		return nil, CompactStats{}, nil
	}
	msgs, err := session.Read(e.Sessions.ID())
	if err != nil {
		return nil, CompactStats{}, err
	}
	return e.compact(ctx, msgs)
}

// compactionModel picks the model used for summarization. FastModel (a cheap
// side-eval model) is preferred so resuming or recovering a huge history
// doesn't pay full prompt-processing on the primary model — critical for slow
// local models where summarizing 190k tokens on the main model can take many
// minutes. Empty FastModel falls back to the default.
func compactionModel(cfg config.Config) string {
	if cfg.FastModel != "" {
		return cfg.FastModel
	}
	return cfg.DefaultModel
}

// compact summarizes msgs with the compaction model and, on success, persists a
// checkpoint to the rollout so a later `bee back` reconstructs the shortened
// history instead of replaying the full raw log. Returns the in-memory result
// for the caller to swap into the live message list.
func (e *Engine) compact(ctx context.Context, msgs []types.Message) ([]types.Message, CompactStats, error) {
	out, stats, err := Compact(ctx, e.Provider, compactionModel(e.Cfg), msgs)
	if err != nil {
		return out, stats, err
	}
	e.persistCheckpoint(ctx, out, stats)
	return out, stats, nil
}

// persistCheckpoint appends a checkpoint marker carrying the summary text and
// the id of the first preserved message. No-op when nothing was compacted or
// the boundary message lacks an id (can't be relocated on resume).
func (e *Engine) persistCheckpoint(ctx context.Context, out []types.Message, stats CompactStats) {
	if e.Sessions == nil || stats.AfterMsgs >= stats.BeforeMsgs || len(out) < 2 {
		return
	}
	preserveFrom := out[1].ID
	if preserveFrom == "" {
		return
	}
	marker := types.Message{
		ID:         newID(),
		Role:       types.RoleUser,
		Content:    out[0].Content,
		Time:       time.Now().UTC(),
		Checkpoint: &types.Checkpoint{PreserveFrom: preserveFrom},
	}
	if err := e.Sessions.Append(ctx, marker); err != nil {
		fmt.Fprintf(os.Stderr, "loop: persist compaction checkpoint: %v\n", err)
	}
}

// PrepareResume compacts a resumed session's seeded history when it already
// exceeds the compaction threshold, BEFORE the first turn runs. Without this,
// the first post-resume turn ships the whole history to the model in one shot —
// on a slow local model that is many minutes of prompt processing that reads as
// a hang. Returns the stats and whether compaction actually ran; on success
// e.InitialMessages is replaced with the shortened history.
func (e *Engine) PrepareResume(ctx context.Context) (CompactStats, bool, error) {
	// state-card profiles skip resume compaction too: the card view bounds the
	// request regardless of history length, so summarizing would only burn time.
	if !e.compactionEnabled() || len(e.InitialMessages) == 0 {
		return CompactStats{}, false, nil
	}
	budget := contextBudget(e.Cfg)
	threshold := scaledCompactThreshold(e.Cfg.Compaction.Threshold, budget)
	// system prompt isn't assembled yet; estimate over history alone.
	if !ShouldAutoCompact("", e.InitialMessages, budget, threshold) {
		return CompactStats{}, false, nil
	}
	out, stats, err := e.compact(ctx, e.InitialMessages)
	if err != nil {
		return stats, false, err
	}
	e.InitialMessages = out
	e.lastInputTokens = 0
	return stats, true, nil
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

// scaledCompactThreshold widens the default compaction threshold for large
// context windows. The fixed 0.75 default fires far too early on 128k-class
// models (sparse MoE: Qwen3.6-35B-A3B-4bit etc.) — at 96k tokens the agent
// still has 32k of breathing room, no reason to compact yet.
//
// Only the default (>=0.75) is widened. An explicit low threshold means the
// user deliberately wants early compaction, so it's honored verbatim — never
// overridden upward. Widening below default would silently ignore the setting.
//
// Formula: derived = max(0.5, 1 - 8000/budget). Keeps at least 8000 tokens of
// headroom for the next turn's output regardless of window size.
//
// budget<=0 or base<=0 returns base unchanged.
func scaledCompactThreshold(base float64, budget int) float64 {
	if budget <= 0 || base <= 0 || base < compactDefaultThreshold {
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

// compactDefaultThreshold mirrors config defaults; values below it are treated
// as deliberate early-compaction requests and never widened.
const compactDefaultThreshold = 0.75
