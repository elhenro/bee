// Triage: queen mode's adaptive front door. Not every task needs a planning +
// multi-reviewer hive — a typo fix, a rename, or a plain question is faster and
// cheaper as a single pass. A cheap side-LLM call (one token) decides, and the
// caller routes small/clear work straight to a normal turn, reserving the hive
// for tasks that actually warrant it.
package hive

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// triageSystem asks for one token: simple or complex. Short to stay cheap and
// fit any model. Mirrors the loop's posture classifier style.
const triageSystem = `You decide how much machinery a coding task needs.

- simple: a small, clear, single-step change or a direct question. One pass, no planning, no multi-reviewer gate. Examples: fix a typo, rename a symbol, a one-line fix, "what does X do?", add a small self-contained function.
- complex: multi-file work, ambiguous scope, design decisions, refactors, or anything risky enough to want planning and review.

Reply with exactly one word: "simple" or "complex". No prose, no punctuation.`

// TriageSimple returns true when task is small/clear enough to skip the hive.
// On any provider error, empty input, or ambiguous reply it returns false
// (complex) — over-planning a trivial task wastes tokens, but under-reviewing a
// risky change is worse, so ambiguity defaults to the full hive.
func TriageSimple(ctx context.Context, p llm.Provider, model, task string) bool {
	if p == nil || strings.TrimSpace(task) == "" {
		return false
	}
	req := llm.Request{
		Model:  model,
		System: triageSystem,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{
				{Type: types.BlockText, Text: task},
			}},
		},
		MaxTokens:   8,
		Temperature: 0,
		Stream:      true,
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return false
	}
	var buf strings.Builder
	for ev := range ch {
		switch ev.Type {
		case llm.EventTextDelta:
			buf.WriteString(ev.Delta)
		case llm.EventError:
			if ev.Err != nil && buf.Len() == 0 {
				return false
			}
		}
	}
	return parseTriageSimple(buf.String())
}

// parseTriageSimple maps raw model text to simple (true) / complex (false).
// Only an explicit "simple" prefix counts; everything else is complex.
func parseTriageSimple(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".\"' \n\t")
	return strings.HasPrefix(s, "simple")
}
