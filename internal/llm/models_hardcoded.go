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
		// chatgpt.com/backend-api/codex backend (subscription auth, /responses
		// only). Models match what the official codex CLI exposes today.
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
