// Knowledge-store adapter wiring for `bee run`. Implements
// loop.KnowledgeStore with a two-phase query: deterministic scoring first,
// optional small-model tag extraction second when phase 1 is thin.
package main

import (
	"context"
	"os"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
)

// knowledgeAdapter satisfies loop.KnowledgeStore using the knowledge
// package. phase 1 of the query is deterministic; phase 2 fires a tiny
// side-LLM call only when phase 1 returns fewer than two candidates.
type knowledgeAdapter struct {
	prov    llm.Provider
	model   string
	dir     string
	enabled bool
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
	if len(recs) >= 2 || k.prov == nil {
		return recs, nil
	}
	// phase 2: ask a small side-LLM for keyword tags and re-score.
	hints, herr := knowledge.ExtractTags(ctx, k.prov, k.model, query)
	if herr != nil || len(hints) == 0 {
		return recs, nil
	}
	return knowledge.Query(ctx, k.dir, query, k.topK, knowledge.Options{HintTags: hints})
}
