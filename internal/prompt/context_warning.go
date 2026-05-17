package prompt

import "fmt"

// WarningThreshold is the usage fraction (0..1) at which a warning is emitted.
const WarningThreshold = 0.70

// FormatContextWarning returns the inline notice to prepend to a tool result.
// Returns "" when ratio < WarningThreshold or limit <= 0.
// The ratio is clamped to [0, 1] before formatting — prevents prompt-injection
// via inflated numbers (a safety-tuned model could otherwise refuse a "200%" claim).
func FormatContextWarning(inputTokens, contextLimit int) string {
	ratio := clampRatio(inputTokens, contextLimit)
	if ratio < WarningThreshold {
		return ""
	}
	pct := int(ratio * 100)
	return fmt.Sprintf("[context at %d%%] you have used %d%% of the context window. summarize aggressively and drop noisy tool output before continuing.\n\n", pct, pct)
}

// ShouldWarn reports whether usage has crossed the threshold. Cheap helper for callers
// that want to dedupe (warn once per session).
func ShouldWarn(inputTokens, contextLimit int) bool {
	return clampRatio(inputTokens, contextLimit) >= WarningThreshold
}

func clampRatio(inputTokens, contextLimit int) float64 {
	if contextLimit <= 0 {
		return 0
	}
	if inputTokens < 0 {
		inputTokens = 0
	}
	r := float64(inputTokens) / float64(contextLimit)
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}
