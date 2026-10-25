// Knowledge-store adapter wiring for `bee run`. Implements
// loop.KnowledgeStore with a two-phase query: deterministic scoring first,
// optional small-model tag extraction second when phase 1 is thin.
package main

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
)

// tagLRUCap bounds entries in the phase-2 tag-extract LRU.
const tagLRUCap = 64

var tagLRU = struct {
	sync.Mutex
	tags  map[string][]string
	order []string // least recently used first
}{tags: map[string][]string{}}

func tagLRUKey(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

// tagLRUTouch moves key to the most-recent end. The caller holds the lock.
func tagLRUTouch(key string) {
	if i := slices.Index(tagLRU.order, key); i >= 0 {
		tagLRU.order = slices.Delete(tagLRU.order, i, i+1)
	}
	tagLRU.order = append(tagLRU.order, key)
}

func tagLRUGet(query string) ([]string, bool) {
	key := tagLRUKey(query)
	if key == "" {
		return nil, false
	}
	tagLRU.Lock()
	defer tagLRU.Unlock()
	tags, ok := tagLRU.tags[key]
	if ok {
		tagLRUTouch(key)
	}
	return slices.Clone(tags), ok
}

func tagLRUPut(query string, tags []string) {
	key := tagLRUKey(query)
	if key == "" {
		return
	}
	tagLRU.Lock()
	defer tagLRU.Unlock()
	tagLRU.tags[key] = slices.Clone(tags)
	tagLRUTouch(key)
	if len(tagLRU.order) > tagLRUCap {
		delete(tagLRU.tags, tagLRU.order[0])
		tagLRU.order = tagLRU.order[1:]
	}
}

// hintBudget caps the phase-2 tag call. it sits in front of the turn's first
// request, so a slow side model gets skipped rather than waited for.
const hintBudget = 400 * time.Millisecond

// knowledgeAdapter satisfies loop.KnowledgeStore using the knowledge
// package. phase 1 of the query is deterministic; phase 2 (opt-in via
// [memory] llm_hints) fires a tiny side-LLM call when phase 1 returns fewer
// than two candidates.
type knowledgeAdapter struct {
	prov    llm.Provider
	model   string
	dir     string
	enabled bool
	hints   bool
	topK    int
}

func newKnowledgeAdapter(p llm.Provider, cfg config.Config) *knowledgeAdapter {
	dir, _ := knowledge.StoreDir()
	topK := cfg.Memory.TopK
	if topK <= 0 {
		topK = 3
	}
	return &knowledgeAdapter{
		prov:    p,
		model:   cfg.DefaultModel,
		dir:     dir,
		enabled: cfg.Memory.Enabled,
		hints:   cfg.Memory.LLMHints,
		topK:    topK,
	}
}

func (k *knowledgeAdapter) Query(ctx context.Context, query string, _ []string) ([]knowledge.Record, error) {
	if !k.enabled || k.dir == "" {
		return nil, nil
	}
	// missing dir is not fatal — first run has no records.
	if _, err := os.Stat(k.dir); err != nil {
		return nil, nil
	}
	files, err := knowledge.ListEntries(k.dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, nil
	}
	// phase 1: deterministic scoring against the user query.
	recs, err := knowledge.Query(ctx, k.dir, query, k.topK, knowledge.Options{})
	if err != nil {
		return nil, err
	}
	if len(recs) >= 2 || k.prov == nil || !k.hints {
		return recs, nil
	}
	// phase 2: ask a small side-LLM for keyword tags and re-score. cache
	// the lookup so repeat queries skip the blocking call.
	hints, ok := tagLRUGet(query)
	if !ok {
		hctx, cancel := context.WithTimeout(ctx, hintBudget)
		defer cancel()
		var herr error
		hints, herr = knowledge.ExtractTags(hctx, k.prov, k.model, query)
		if herr != nil || len(hints) == 0 {
			return recs, nil
		}
		tagLRUPut(query, hints)
	}
	if len(hints) == 0 {
		return recs, nil
	}
	return knowledge.Query(ctx, k.dir, query, k.topK, knowledge.Options{HintTags: hints})
}
