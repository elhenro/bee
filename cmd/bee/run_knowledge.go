// Knowledge-store adapter wiring for `bee run`. Implements
// loop.KnowledgeStore with a two-phase query: deterministic scoring first,
// optional small-model tag extraction second when phase 1 is thin.
package main

import (
	"container/list"
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
)

// tagLRUCap bounds entries in the phase-2 tag-extract LRU.
const tagLRUCap = 64

// tagLRUEntry pairs the normalized key with the extracted tags so list
// elements are self-describing on eviction.
type tagLRUEntry struct {
	key  string
	tags []string
}

var (
	tagLRUMu    sync.Mutex
	tagLRUList  = list.New()
	tagLRUIndex = map[string]*list.Element{}
)

func tagLRUKey(query string) string {
	return strings.ToLower(strings.TrimSpace(query))
}

func tagLRUGet(query string) ([]string, bool) {
	key := tagLRUKey(query)
	if key == "" {
		return nil, false
	}
	tagLRUMu.Lock()
	defer tagLRUMu.Unlock()
	el, ok := tagLRUIndex[key]
	if !ok {
		return nil, false
	}
	tagLRUList.MoveToFront(el)
	entry := el.Value.(tagLRUEntry)
	out := append([]string(nil), entry.tags...)
	return out, true
}

func tagLRUPut(query string, tags []string) {
	key := tagLRUKey(query)
	if key == "" {
		return
	}
	tagLRUMu.Lock()
	defer tagLRUMu.Unlock()
	if el, ok := tagLRUIndex[key]; ok {
		el.Value = tagLRUEntry{key: key, tags: append([]string(nil), tags...)}
		tagLRUList.MoveToFront(el)
		return
	}
	el := tagLRUList.PushFront(tagLRUEntry{key: key, tags: append([]string(nil), tags...)})
	tagLRUIndex[key] = el
	for tagLRUList.Len() > tagLRUCap {
		back := tagLRUList.Back()
		if back == nil {
			break
		}
		tagLRUList.Remove(back)
		delete(tagLRUIndex, back.Value.(tagLRUEntry).key)
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
