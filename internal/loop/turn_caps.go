package loop

import (
	"context"
	"fmt"
	"os"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/types"
)

// maxBudgetRecoveries bounds how many times one Run may auto-recover from the
// token-budget cap before hard-stopping. Each recovery compacts history and
// re-arms the cumulative counter, so total spend is bounded at roughly
// (maxBudgetRecoveries+1)×tokenBudget — the cap still guards runaway cost, it
// just compacts and continues instead of stopping on the first hit.
const maxBudgetRecoveries = 3

// computeBudgetCaps returns the adaptive token budget and stall cap for the
// current Run. tokenBudget=0 means unknown context window → disabled. Both
// caps fire BEFORE the iter ceiling so wasteful runs stop on real cost
// (tokens) or real stall (read-only streak) rather than running out the
// arbitrary iter count.
//   tokenBudget: 10× the model's context window.
//   stallCap:    3× profile NoMutationStallThreshold, default 8.
func computeBudgetCaps(cfg config.Config) (tokenBudget, stallCap int) {
	tokenBudget = 10 * contextBudget(cfg)
	stallCap = 8
	if t := config.ActiveProfile(cfg).NoMutationStallThreshold; t > 0 {
		stallCap = t * 3
	}
	return
}

// handleBudgetCaps enforces the per-Run caps at the top of each iteration.
// The read-only stall cap is a hard stop — compaction can't unwedge an
// explore-loop. The token-budget cap instead AUTO-RECOVERS up to
// maxBudgetRecoveries times: compact history, reset the cumulative counter,
// re-arm the token warnings, and continue. Only once the recovery budget is
// spent does it hard-stop. Returns a non-nil error only on hard stop; callers
// return it as the Run error verbatim. msgs is mutated in place on recovery.
func (e *Engine) handleBudgetCaps(ctx context.Context, msgs *[]types.Message, currentIter, tokenBudget, stallCap int) error {
	// read-only stall: model kept calling reads for stallCap iters without any
	// mutation — almost always stuck in an explore-loop. no recovery.
	if e.noMutationStreak >= stallCap {
		return fmt.Errorf("loop: %d read-only iters with no edits, stopping — type 'continue' to resume", e.noMutationStreak)
	}
	// token budget: cumulative input+output across iterations. only enforced
	// when the model's context window is known so unknown-model runs aren't
	// bounded by a fabricated number.
	spent := e.cumInputTokens + e.cumOutputTokens
	if tokenBudget <= 0 || spent <= tokenBudget {
		return nil
	}
	// recovery exhausted (or disabled) → hard stop.
	if e.budgetRecoveries >= maxBudgetRecoveries || !e.Cfg.Compaction.Enabled {
		return fmt.Errorf("loop: hit token budget (%d > %d tokens, %d iters, %d auto-compactions) — type 'continue' to resume",
			spent, tokenBudget, currentIter, e.budgetRecoveries)
	}
	// auto-recover: force-compact history, re-arm the cap, continue. compaction
	// shrinks per-turn cost going forward; resetting the cumulative counter
	// re-arms the guard for another budget's worth of work.
	e.budgetRecoveries++
	if compacted, _, cerr := Compact(ctx, e.Provider, e.Cfg.DefaultModel, *msgs); cerr == nil {
		*msgs = compacted
	} else {
		fmt.Fprintf(os.Stderr, "loop: budget-recovery compact failed: %v\n", cerr)
	}
	e.cumInputTokens = 0
	e.cumOutputTokens = 0
	e.lastInputTokens = 0
	e.warnedTokenHalf = false
	e.warnedTokenEighty = false
	fmt.Fprintf(os.Stderr, "loop: token budget hit (%d/%d) — compacted and continued (recovery %d/%d)\n",
		spent, tokenBudget, e.budgetRecoveries, maxBudgetRecoveries)
	return nil
}
