package loop

import (
	"fmt"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// injectIterAndTokenWarnings prepends one-shot warning prefixes to the next
// tool-result block when iter/token/stall thresholds cross. Each warning
// fires at most once per Run (dedup via Engine flags).
func injectIterAndTokenWarnings(e *Engine, blocks []types.ContentBlock, currentIter, maxIter, tokenBudget int) []types.ContentBlock {
	// context-window warning: if usage crosses threshold, prepend a one-shot
	// notice so the model summarizes/drops noise on the following turn.
	if !e.warnedContext {
		limit := contextBudget(e.Cfg)
		if w := prompt.FormatContextWarning(e.lastInputTokens, limit); w != "" {
			blocks = prependWarningToToolResult(blocks, w)
			e.warnedContext = true
		}
	}
	// iteration warnings; each fires at most once per Run.
	if !e.warnedIterHalf && currentIter*2 >= maxIter {
		w := fmt.Sprintf("[iter %d/%d] half the budget spent. summarize progress; commit edits or stop if stuck.\n\n", currentIter, maxIter)
		blocks = prependWarningToToolResult(blocks, w)
		e.warnedIterHalf = true
	}
	if !e.warnedIterEighty && currentIter*5 >= maxIter*4 {
		w := fmt.Sprintf("[iter %d/%d] near iter cap. finish current edit or stop and ask user.\n\n", currentIter, maxIter)
		blocks = prependWarningToToolResult(blocks, w)
		e.warnedIterEighty = true
	}
	// token-budget warnings mirror the iter-cap warnings so the model hears
	// about cost pressure separately from iter pressure.
	if tokenBudget > 0 {
		spent := e.cumInputTokens + e.cumOutputTokens
		if !e.warnedTokenHalf && spent*2 >= tokenBudget {
			w := fmt.Sprintf("[tokens %d/%d] half the token budget spent. summarize and commit edits.\n\n", spent, tokenBudget)
			blocks = prependWarningToToolResult(blocks, w)
			e.warnedTokenHalf = true
		}
		if !e.warnedTokenEighty && spent*5 >= tokenBudget*4 {
			w := fmt.Sprintf("[tokens %d/%d] near token cap. finish current edit or stop.\n\n", spent, tokenBudget)
			blocks = prependWarningToToolResult(blocks, w)
			e.warnedTokenEighty = true
		}
	}
	// stall warning is opt-in: profile must set a positive threshold.
	if t := config.ActiveProfile(e.Cfg).NoMutationStallThreshold; t > 0 {
		if !e.warnedStall && e.noMutationStreak >= t {
			w := fmt.Sprintf("[stall] %d read-only iters; commit edits when ready.\n\n", e.noMutationStreak)
			blocks = prependWarningToToolResult(blocks, w)
			e.warnedStall = true
		}
	}
	return blocks
}
