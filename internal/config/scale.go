package config

// ScaleProfileForContext widens tiny-profile budgets when the active model
// reports a context window much larger than the tiny default. Sparse MoE
// models like Qwen3.6-35B-A3B-4bit advertise 128k context but the canned
// tiny profile pins SystemPromptBudget=3000 / ToolOutputTokens=1500,
// leaving ~95% of the window unused.
//
// Heuristic: at ctxWindow > 16k, scale SystemPromptBudget to min(ctx*0.05,
// 8000) and ToolOutputTokens to min(ctx*0.02, 4000). The 4-tool surface just
// gets room to breathe.
//
// Returns the input unchanged when profile isn't tiny or ctxWindow ≤ 16k.
// Caller passes the resolved profile name (after "auto" → "tiny" mapping).
func ScaleProfileForContext(p Profile, profileName string, ctxWindow int) Profile {
	if profileName != "tiny" || ctxWindow <= 16000 {
		return p
	}
	p.SystemPromptBudget = max(p.SystemPromptBudget, min(int(float64(ctxWindow)*0.05), 8000))
	p.ToolOutputTokens = max(p.ToolOutputTokens, min(int(float64(ctxWindow)*0.02), 4000))
	return p
}
