package llm

import (
	"strings"
	"sync"
)

// ctxCache holds live-learned context lengths for model ids. Populated by
// RememberContextLength (called from ListModels' storeCache and the ollama
// /api/show prewarm goroutine). RLock-cheap reads keep ContextWindow on the
// hot path for every token-budget check the loop runs.
var ctxCache = struct {
	sync.RWMutex
	m map[string]int
}{m: map[string]int{}}

// RememberContextLength records a live-learned context window for modelID.
// Both the raw id and its trailing path segment are stored so future
// lookups resolve regardless of which form ContextWindow gets.
func RememberContextLength(modelID string, n int) {
	if modelID == "" || n <= 0 {
		return
	}
	ctxCache.Lock()
	defer ctxCache.Unlock()
	ctxCache.m[modelID] = n
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 {
		ctxCache.m[modelID[idx+1:]] = n
	}
}

// ResetLiveContextLengths drops every learned entry. Tests call this.
func ResetLiveContextLengths() {
	ctxCache.Lock()
	defer ctxCache.Unlock()
	ctxCache.m = map[string]int{}
}

// ContextWindow returns a best-effort context length (in tokens) for the
// given model id. Live API-learned values win over the hardcoded table.
// Tries the exact id, then the trailing path segment so OpenRouter ids
// ("anthropic/claude-sonnet-4-6") resolve to the bare model name. Returns
// 0 when unknown — TUI treats that as "no fill indicator".
func ContextWindow(modelID string) int {
	if modelID == "" {
		return 0
	}
	ctxCache.RLock()
	if c, ok := ctxCache.m[modelID]; ok {
		ctxCache.RUnlock()
		return c
	}
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 {
		if c, ok := ctxCache.m[modelID[idx+1:]]; ok {
			ctxCache.RUnlock()
			return c
		}
	}
	ctxCache.RUnlock()
	if c, ok := contextLengths[modelID]; ok {
		return c
	}
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 {
		if c, ok := contextLengths[modelID[idx+1:]]; ok {
			return c
		}
	}
	return 0
}

// contextLengths is a curated table of known model context windows. Keep in
// sync with cost.prices and the picker's /models output.
var contextLengths = map[string]int{
	// Anthropic
	"claude-haiku-4-5":  200000,
	"claude-sonnet-4-5": 200000,
	"claude-sonnet-4-6": 200000,
	"claude-opus-4-1":   200000,
	"claude-opus-4-7":   200000,
	// OpenAI
	"gpt-4o":      128000,
	"gpt-4o-mini": 128000,
	"o1":          200000,
	"o1-mini":     128000,
	"o3-mini":     200000,
	// DeepSeek
	"deepseek-v4-flash": 1000000,
	"deepseek-chat":     65536,
	"deepseek-reasoner": 65536,
	// Google
	"gemini-2.0-flash": 1000000,
	"gemini-2.5-flash": 1000000,
	"gemini-2.5-pro":   2000000,
	// Groq / Llama
	"llama-3.3-70b-versatile": 131072,
	"llama-3.1-8b-instant":    131072,
}

// thinkingModelSubstrings names model families known to honor a reasoning
// budget. Matched case-insensitively against both the full id and the trailing
// path segment so openrouter-style ids ("openai/o3-mini") resolve. Keep entries
// substring-safe: "o1" must not match "claude-opus-4-1" — guard those with
// hyphens. New families: add the smallest unique distinguishing fragment.
var thinkingModelSubstrings = []string{
	// OpenAI o-series (o1, o3, o4-mini …) — substring "/o" or "-o" guarded by
	// matching the prefix-after-vendor form below.
	"o1", "o3", "o4-mini",
	// GPT-5 family supports reasoning_effort.
	"gpt-5",
	// Anthropic 4.x extended thinking.
	"claude-opus-4", "claude-sonnet-4", "claude-haiku-4",
	// DeepSeek reasoners (reasoner, v3.1+, v4 flash/full).
	"deepseek-reasoner", "deepseek-r1", "deepseek-v3.1", "deepseek-v3.2", "deepseek-v4",
	// Gemini 2.5 family (thinkingBudget).
	"gemini-2.5",
	// Qwen reasoning variants.
	"qwq", "qwen3-thinking",
	// Z.AI GLM thinking tier.
	"glm-4.6",
	// xAI grok reasoning.
	"grok-3", "grok-4",
	// Moonshot kimi-k2 reasoner.
	"kimi-k2",
}

// SupportsThinking reports whether modelID belongs to a family that honors a
// reasoning_effort / thinking-budget request. Best-effort: substring match
// against thinkingModelSubstrings on both the full id and trailing path
// segment. Unknown models return false → ThinkingAuto resolves to off so we
// don't break non-reasoning providers that choke on the field.
func SupportsThinking(modelID string) bool {
	if modelID == "" {
		return false
	}
	id := strings.ToLower(modelID)
	tail := id
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		tail = id[idx+1:]
	}
	for _, s := range thinkingModelSubstrings {
		if strings.Contains(id, s) || strings.Contains(tail, s) {
			return true
		}
	}
	return false
}

// hardcodedModels returns a curated list for wire families whose /models
// endpoint is unavailable or gated (notably Anthropic).
func hardcodedModels(wireAPI string) []Model {
	switch wireAPI {
	case "anthropic":
		return []Model{
			{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", ContextLength: 200000},
			{ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", ContextLength: 200000},
			{ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", ContextLength: 200000},
			{ID: "claude-opus-4-1", Name: "Claude Opus 4.1", ContextLength: 200000},
		}
	default:
		return []Model{}
	}
}

// hardcodedFallback maps provider *names* to canned lists when the live
// /models endpoint fails. Keeps the picker useful in offline dev.
func hardcodedFallback(name string) []Model {
	switch name {
	case "anthropic", "claude":
		return hardcodedModels("anthropic")
	case "openai":
		return []Model{
			{ID: "gpt-4o", Name: "GPT-4o"},
			{ID: "gpt-4o-mini", Name: "GPT-4o mini"},
			{ID: "o1", Name: "o1"},
			{ID: "o1-mini", Name: "o1 mini"},
		}
	case "chatgpt":
		// chatgpt.com subscription backend (auth, /responses only). Curated
		// list of models exposed by the responses endpoint.
		return []Model{
			{ID: "gpt-5-codex", Name: "GPT-5 Codex", ContextLength: 272000},
			{ID: "gpt-5", Name: "GPT-5", ContextLength: 272000},
			{ID: "gpt-5-pro", Name: "GPT-5 Pro", ContextLength: 272000},
			{ID: "gpt-5-codex-mini", Name: "GPT-5 Codex mini", ContextLength: 272000},
		}
	case "ollama":
		return []Model{
			{ID: "llama3.1:8b", Name: "Llama 3.1 8B"},
			{ID: "qwen2.5-coder:7b", Name: "Qwen2.5 Coder 7B"},
		}
	}
	return nil
}
