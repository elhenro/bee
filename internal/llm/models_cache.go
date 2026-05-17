package llm

import (
	"sync"
	"time"
)

// modelCacheTTL is how long a successful /models response stays warm before
// ListModels re-fetches. Picker UI calls this on every open, so a short TTL
// keeps it snappy without hammering upstream.
const modelCacheTTL = 10 * time.Minute

type cacheEntry struct {
	models []Model
	at     time.Time
}

var (
	modelCache   = map[string]cacheEntry{}
	modelCacheMu sync.Mutex
)

// ClearModelCache drops every cached entry. Tests call this between cases.
func ClearModelCache() {
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()
	modelCache = map[string]cacheEntry{}
}

func lookupCache(name string) ([]Model, bool) {
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()
	e, ok := modelCache[name]
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > modelCacheTTL {
		delete(modelCache, name)
		return nil, false
	}
	// return a copy so callers can't mutate the cached slice
	out := make([]Model, len(e.models))
	copy(out, e.models)
	return out, true
}

func storeCache(name string, models []Model) {
	modelCacheMu.Lock()
	cp := make([]Model, len(models))
	copy(cp, models)
	modelCache[name] = cacheEntry{models: cp, at: time.Now()}
	modelCacheMu.Unlock()
	// seed the package-wide live ctx cache so ContextWindow can answer for
	// any model the API surfaced — not just the curated hardcoded table.
	for _, m := range models {
		if m.ContextLength > 0 {
			RememberContextLength(m.ID, m.ContextLength)
		}
	}
}
