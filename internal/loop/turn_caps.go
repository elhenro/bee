package loop

import (
	"fmt"

	"github.com/elhenro/bee/internal/config"
)

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

// checkEarlyStop returns a non-nil error when token-budget or stall caps
// have been crossed. caller returns it as the Run error verbatim.
func checkEarlyStop(e *Engine, currentIter, tokenBudget, stallCap int) error {
	// early-stop: token budget exhausted (cumulative input + output across
	// iterations). only enforced when the model's context window is known
	// so unknown-model runs aren't bounded by a fabricated number.
	if tokenBudget > 0 && (e.cumInputTokens+e.cumOutputTokens) > tokenBudget {
		return fmt.Errorf("loop: hit token budget (%d > %d tokens, %d iters) — type 'continue' to resume",
			e.cumInputTokens+e.cumOutputTokens, tokenBudget, currentIter)
	}
	// early-stop: read-only stall. model kept calling reads for stallCap
	// iters without any mutation — almost always stuck in explore-loop.
	if e.noMutationStreak >= stallCap {
		return fmt.Errorf("loop: %d read-only iters with no edits, stopping — type 'continue' to resume", e.noMutationStreak)
	}
	return nil
}
