package knowledge

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// MaxResults caps the records Query returns regardless of caller intent.
const MaxResults = 5

// Options tunes query behavior. zero values pick sensible defaults.
type Options struct {
	// HintTags are query-derived tags merged with the user-supplied ones.
	// populate from a side query when you want phase-2 hint extraction.
	HintTags []string
	// Now overrides the wall clock for testing. zero = time.Now().
	Now time.Time
	// Exclude paths that already surfaced this turn so the budget goes to
	// fresh candidates.
	Exclude map[string]bool
}

// Query scores every entry in dir against the user's plain-text query and
// returns the top-N records (header + body). scoring is deterministic and
// purely structural — no LLM call is made. Callers wanting phase-2 hint
// extraction supply Options.HintTags from a side query.
//
// returns (nil, nil) for missing or empty stores so callers can fall
// through without first stat'ing.
func Query(ctx context.Context, dir, userQuery string, limit int, opts Options) ([]Record, error) {
	if limit <= 0 || limit > MaxResults {
		limit = MaxResults
	}
	entries, err := ScanStore(ctx, dir)
	if err != nil {
		return nil, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	sc := scorer{
		query: tokenize(userQuery),
		// 2-char tags (go, ui, db) match whole query words; the general tokens
		// drop them as noise.
		short: tokenizeMin(userQuery, 2),
		hints: map[string]bool{},
	}
	for _, t := range opts.HintTags {
		if t = strings.TrimSpace(strings.ToLower(t)); t != "" {
			sc.hints[t] = true
		}
	}

	type scored struct {
		entry Entry
		score int
	}
	var candidates []scored
	for _, e := range entries {
		if (!e.ExpiresAt.IsZero() && !e.ExpiresAt.After(now)) || opts.Exclude[e.Path] {
			continue
		}
		candidates = append(candidates, scored{e, sc.score(e)})
	}
	slices.SortStableFunc(candidates, func(a, b scored) int {
		return cmp.Or(b.score-a.score, b.entry.Modified.Compare(a.entry.Modified))
	})
	if len(candidates) == 0 {
		return nil, nil
	}

	out := make([]Record, 0, min(limit, len(candidates)))
	for _, c := range candidates[:min(limit, len(candidates))] {
		body, _ := Body(c.entry.Path)
		out = append(out, Record{Entry: c.entry, Body: body})
	}
	return out, nil
}

// scorer ranks entries against one query: its tokens, the 2-char-inclusive
// tokens and the side-query hint tags.
type scorer struct {
	query, short, hints map[string]bool
}

func (s scorer) score(e Entry) int {
	n := e.Priority + kindBoost(e.Tags)
	for _, t := range e.Tags {
		if s.hints[t] {
			n += 2
		}
		// slug-style tags like "testing-policy" should still hit plain
		// queries like "testing"; tokenize splits + keeps the joined form.
		if s.query[t] || (len(t) == 2 && s.short[t]) || overlap(s.query, tokenize(t)) > 0 {
			n++
		}
	}
	return n + 2*overlap(s.query, tokenize(e.Name)) + overlap(s.query, tokenize(e.Description))
}

// overlap counts the tokens of a that also appear in b.
func overlap(a, b map[string]bool) int {
	n := 0
	for tok := range a {
		if b[tok] {
			n++
		}
	}
	return n
}

// ExtractTags drives a side-channel LLM call to map a free-text query onto
// 1-3 tag keywords. callers feed the result back into Query via Options.
// errors degrade quietly to (nil, err) so the agent loop can keep going on
// the deterministic phase-1 score.
func ExtractTags(ctx context.Context, p llm.Provider, model, userQuery string) ([]string, error) {
	if p == nil {
		return nil, nil
	}
	req := llm.Request{
		Model:  model,
		System: "You extract short, lowercase, hyphenated keyword tags from a question. No prose, one tag per line, at most three lines.",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{
				{Type: types.BlockText, Text: trimQuery(userQuery)},
			}},
		},
		MaxTokens:   64,
		Temperature: 0,
		Stream:      true,
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	var buf strings.Builder
	for ev := range ch {
		switch ev.Type {
		case llm.EventTextDelta:
			buf.WriteString(ev.Delta)
		case llm.EventError:
			if ev.Err != nil && buf.Len() == 0 {
				return nil, ev.Err
			}
		}
	}
	return parseTagLines(buf.String()), nil
}

func trimQuery(s string) string {
	const limit = 400
	if len(s) <= limit {
		return s
	}
	return types.CutBytes(s, limit) + "…"
}

func parseTagLines(s string) []string {
	var out []string
	for _, raw := range strings.Split(s, "\n") {
		// strip common list prefixes models still emit
		raw = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "- "), "* ")
		raw = strings.ToLower(strings.Trim(raw, "`\""))
		if tagPattern.MatchString(raw) && !slices.Contains(out, raw) {
			out = append(out, raw)
			if len(out) == 3 {
				break
			}
		}
	}
	return out
}

// kindBoost adds a small constant to the score of records carrying a
// well-known taxonomy tag. The taxonomy mirrors the user/feedback/project/
// reference memory split; guidance entries are standing rules that should rank
// above plain priority noise, personal preferences come next, project and
// external context add no extra weight beyond the user-set Priority field.
func kindBoost(tags []string) int {
	boost := 0
	for _, t := range tags {
		switch t {
		case TagGuidance:
			boost = max(boost, 2)
		case TagPersonal:
			boost = max(boost, 1)
		}
	}
	return boost
}

// tokenize splits s on non-word boundaries, lowercases, and drops short
// noise tokens. hyphens and underscores yield both the joined slug form
// and each split sub-token so natural and slug-style queries both hit.
func tokenize(s string) map[string]bool { return tokenizeMin(s, 3) }

// tokenizeMin is tokenize with a configurable minimum token length.
func tokenizeMin(s string, minLen int) map[string]bool {
	out := map[string]bool{}
	emit := func(t string) {
		if len(t) >= minLen {
			out[t] = true
		}
	}
	isSep := func(r rune) bool { return r == '-' || r == '_' }
	isWord := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || isSep(r)
	}
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return !isWord(r) }) {
		w = strings.ToLower(w)
		emit(w)
		// also emit sub-tokens for slug-style joined forms.
		for _, part := range strings.FieldsFunc(w, isSep) {
			emit(part)
		}
	}
	return out
}
