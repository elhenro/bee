// Package knowledge_search implements the knowledge_search tool: on-demand
// lookup of the bee knowledge store from inside an agent turn. used when
// the model wants to recall past observations, decisions, or stored
// preferences without waiting for the passive selection layer to inject
// them.
package knowledge_search

import (
	"context"
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "knowledge_search"

// Tool searches the knowledge store on demand.
type Tool struct {
	prov  llm.Provider
	model string
	dir   string
	topK  int
}

// New constructs the tool. topK defaults to 3 when ≤0.
func New(prov llm.Provider, model, dir string, topK int) *Tool {
	if topK <= 0 {
		topK = 3
	}
	return &Tool{prov: prov, model: model, dir: dir, topK: topK}
}

// Spec advertises the tool to the model.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          toolName,
		Description:   "Search the bee knowledge store for stored notes, preferences, decisions, and project context. Use when you need to recall something the agent has previously recorded. Returns the full body of matching records.",
		PromptSnippet: "Search bee's knowledge store",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "What to search for: keywords, topic, or a short description of the information you need.",
				},
			},
			"required": []string{"query"},
		},
	}
}

// Run executes the search and returns the matched records as text.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	query, _ := in["query"].(string)
	if strings.TrimSpace(query) == "" {
		return tools.Result{Content: "missing or empty 'query' field", IsError: true}, nil
	}
	if t.dir == "" {
		return tools.Result{Content: "knowledge store not configured", IsError: true}, nil
	}

	records, err := knowledge.Query(ctx, t.dir, query, t.topK, knowledge.Options{})
	if err != nil {
		return tools.Result{Content: fmt.Sprintf("search failed: %v", err), IsError: true}, nil
	}
	// phase 2: fire the side-LLM whenever phase 1 didn't fill the top-K slate.
	if len(records) < t.topK && t.prov != nil {
		hints, hErr := knowledge.ExtractTags(ctx, t.prov, t.model, query)
		if hErr == nil && len(hints) > 0 {
			rerun, qErr := knowledge.Query(ctx, t.dir, query, t.topK, knowledge.Options{HintTags: hints})
			if qErr == nil && len(rerun) > 0 {
				records = rerun
			}
		}
	}

	if len(records) == 0 {
		return tools.Result{Content: "no matching records found"}, nil
	}

	var b strings.Builder
	for i, r := range records {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		tagFrag := ""
		if len(r.Tags) > 0 {
			tagFrag = "[" + strings.Join(r.Tags, ", ") + "] "
		}
		age := knowledge.AgeSince(r.Modified)
		fmt.Fprintf(&b, "# %s%s (priority %d, %s)\n%s", tagFrag, r.Name, r.Priority, age, r.Body)
		if note := knowledge.StalenessNote(r.ExpiresAt); note != "" {
			fmt.Fprintf(&b, "\n\n> %s", note)
		}
	}

	return tools.Result{Content: b.String()}, nil
}
