package llm

import "strings"

// IsQwen3HybridThinking reports whether modelID names a Qwen3 model that
// supports the `/think` `/no_think` system-prompt toggle. Unlike the
// reasoning_effort wire path (covered by SupportsThinking), Qwen3 hybrid
// inference servers (omlx/lmstudio/ollama running qwen3-*-a3b, qwen3-coder,
// qwen3-235b, etc.) consume the toggle as a literal token in the prompt.
//
// Excludes already-explicit thinking variants (qwq, qwen3-thinking,
// qwen3-reasoner): those models think unconditionally, no toggle needed.
//
// Heuristic: substring "qwen3" or "qwen-3" present AND no explicit thinking
// suffix. Matches sparse-MoE (a3b, a7b), dense coder (qwen3-coder-30b), and
// flagship (qwen3-235b) families.
func IsQwen3HybridThinking(modelID string) bool {
	if modelID == "" {
		return false
	}
	id := strings.ToLower(modelID)
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		id = id[idx+1:]
	}
	hasFamily := strings.Contains(id, "qwen3") || strings.Contains(id, "qwen-3")
	if !hasFamily {
		return false
	}
	// already-explicit thinking variant — no toggle needed.
	for _, exclude := range []string{"qwq", "thinking", "reasoner"} {
		if strings.Contains(id, exclude) {
			return false
		}
	}
	return true
}

// Qwen3ThinkingHint maps a resolved Thinking level into the literal toggle
// token Qwen3 hybrid models consume. ThinkingOff / ThinkingLow → `/no_think`
// (skip reasoning trace entirely). ThinkingMedium / High / Max → `/think`.
// Empty string means "no toggle" — caller skips injection.
func Qwen3ThinkingHint(t Thinking) string {
	switch t {
	case ThinkingOff, ThinkingLow:
		return "/no_think"
	case ThinkingMedium, ThinkingHigh, ThinkingMax:
		return "/think"
	}
	return ""
}
