package loop

import (
	"fmt"

	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// injectIterAndTokenWarnings prepends one-shot warning prefixes to the next
// tool-result block when iter/token thresholds cross. Each warning
// fires at most once per Run (dedup via Engine flags).
func injectIterAndTokenWarnings(e *Engine, blocks []types.ContentBlock, currentIter, maxIter, tokenBudget int) []types.ContentBlock {
	r := e.run
	// context-window warning: if usage crosses threshold, prepend a one-shot
	// notice so the model summarizes/drops noise on the following turn.
	if !r.warnedContext {
		if w := prompt.FormatContextWarning(r.lastInputTokens, contextBudget(e.Cfg)); w != "" {
			blocks = prependWarningToToolResult(blocks, w)
			r.warnedContext = true
		}
	}
	// warn prefixes the message once the condition holds; flag dedupes it per Run.
	warn := func(flag *bool, due bool, format string, args ...any) {
		if *flag || !due {
			return
		}
		blocks = prependWarningToToolResult(blocks, fmt.Sprintf(format, args...))
		*flag = true
	}
	// iteration warnings. Skipped entirely when the cap is lifted (maxIter <= 0)
	// — there's no ceiling to warn about; the token-budget guard
	// below carries the load instead.
	if maxIter > 0 {
		warn(&r.warnedIterHalf, currentIter*2 >= maxIter,
			"[iter %d/%d] half the budget spent. summarize progress; commit edits or stop if stuck.\n\n", currentIter, maxIter)
		warn(&r.warnedIterEighty, currentIter*5 >= maxIter*4,
			"[iter %d/%d] near iter cap. finish current edit or stop and ask user.\n\n", currentIter, maxIter)
	}
	// token-budget warnings mirror the iter-cap warnings so the model hears
	// about cost pressure separately from iter pressure.
	if tokenBudget > 0 {
		spent := r.cumInputTokens + r.cumOutputTokens
		warn(&r.warnedTokenHalf, spent*2 >= tokenBudget,
			"[tokens %d/%d] half the token budget spent. summarize and commit edits.\n\n", spent, tokenBudget)
		warn(&r.warnedTokenEighty, spent*5 >= tokenBudget*4,
			"[tokens %d/%d] near token cap. finish current edit or stop.\n\n", spent, tokenBudget)
	}
	// cross-turn reasoning loop: the model rehashed near-identical reasoning for
	// reasoningDupBailAt turns running. independent of mutation state — fires
	// even while it makes token tool calls each turn. one-shot per crossing.
	warn(&r.warnedReasoningDup, r.reasoningDupStreak >= reasoningDupBailAt,
		"[loop] your last %d turns repeated near-identical reasoning without progress. stop re-deriving the same analysis — take a concrete action, or call `escalate` with a reason to exit cleanly.\n\n", r.reasoningDupStreak+1)
	return blocks
}
