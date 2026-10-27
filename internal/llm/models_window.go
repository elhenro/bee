package llm

import (
	"cmp"
	"maps"
	"regexp"
	"strings"
	"sync"
)

// claudeDefaultWindow is the window assumed for a claude id with no table
// entry. Deliberately conservative: compacting early is harmless, a window of
// 0 silences auto-compact, budget checks and warnings for the whole session.
const claudeDefaultWindow = 200000

// ctxCache holds live-learned context lengths for model ids. Populated by
// RememberContextLength (called from ListModels' storeCache and the ollama
// /api/show prewarm goroutine). RLock-cheap reads keep ContextWindow on the
// hot path for every token-budget check the loop runs.
var ctxCache = struct {
	sync.RWMutex
	m map[string]int
}{m: map[string]int{}}

// modelTail is the last path segment of a model id, so routed ids
// ("vendor/model-name") resolve to the bare model name.
func modelTail(modelID string) string {
	return modelID[strings.LastIndex(modelID, "/")+1:]
}

// snapshotSuffixRe matches a dated snapshot ("-20250929", "-2024-08-06") or a
// moving alias ("-latest") at the end of an id.
var snapshotSuffixRe = regexp.MustCompile(`-(latest|\d{8}|\d{4}-\d{2}-\d{2})$`)

// canonicalModelID drops the snapshot or alias suffix so a dated id resolves to
// its family entry. claude ids also fold dots to dashes ("claude-sonnet-4.5").
func canonicalModelID(tail string) string {
	tail = snapshotSuffixRe.ReplaceAllString(tail, "")
	if strings.HasPrefix(strings.ToLower(tail), "claude-") {
		tail = strings.ReplaceAll(tail, ".", "-")
	}
	return tail
}

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
	ctxCache.m[modelTail(modelID)] = n
}

// ResetLiveContextLengths drops every learned entry. Tests call this.
func ResetLiveContextLengths() {
	ctxCache.Lock()
	defer ctxCache.Unlock()
	ctxCache.m = map[string]int{}
}

// ContextWindow returns a best-effort context length (in tokens) for the
// given model id. Live API-learned values win over the hardcoded table. Each
// source is tried with the exact id, its trailing path segment and the
// snapshot-stripped form; the table then falls back to its longest family
// prefix, and unknown claude ids get claudeDefaultWindow. Returns 0 for any
// other unknown id, which the TUI shows as "no fill indicator".
func ContextWindow(modelID string) int {
	if modelID == "" {
		return 0
	}
	tail := modelTail(modelID)
	canon := canonicalModelID(tail)
	keys := [3]string{modelID, tail, canon}

	ctxCache.RLock()
	c := firstHit(ctxCache.m, keys)
	ctxCache.RUnlock()
	if c > 0 {
		return c
	}
	known := hardcodedWindows()
	if c = firstHit(known, keys); c > 0 {
		return c
	}
	if c = familyWindow(known, canon); c > 0 {
		return c
	}
	if strings.HasPrefix(strings.ToLower(canon), "claude-") {
		return claudeDefaultWindow
	}
	return 0
}

func firstHit(m map[string]int, keys [3]string) int {
	for _, k := range keys {
		if c := m[k]; c > 0 {
			return c
		}
	}
	return 0
}

// familyWindow is the window of the longest table key that id extends on a
// segment boundary ("o1-preview" extends "o1"; "o10" does not).
func familyWindow(known map[string]int, id string) int {
	best, window := "", 0
	for k, w := range known {
		if len(k) > len(best) && strings.HasPrefix(id, k) && len(id) > len(k) && (id[len(k)] == '-' || id[len(k)] == ':') {
			best, window = k, w
		}
	}
	return window
}

// hardcodedWindows is the curated table plus the windows of the offline
// fallback model lists, so a picker entry always resolves. The table wins on
// a conflict.
var hardcodedWindows = sync.OnceValue(func() map[string]int {
	m := maps.Clone(contextLengths)
	for _, provider := range []string{"anthropic", "openai", "chatgpt", "gemini", "minimax"} {
		for _, md := range hardcodedFallback(provider) {
			if md.ContextLength > 0 {
				m[md.ID] = cmp.Or(m[md.ID], md.ContextLength)
			}
		}
	}
	return m
})
