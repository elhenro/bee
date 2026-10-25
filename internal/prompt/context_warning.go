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

func clampRatio(inputTokens, contextLimit int) float64 {
	if contextLimit <= 0 {
		return 0
	}
	return min(max(float64(inputTokens)/float64(contextLimit), 0), 1)
}
