package knowledge

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// MaxResults caps the records Query returns regardless of caller intent.
const MaxResults = 5

// hintExtractPrompt asks a side-channel model for 1-3 keyword tags. it
// returns plain text — one tag per line — which is easier to parse than
// JSON for tiny models and adapts naturally to local backends.
const hintExtractPrompt = `Pick 1-3 short keyword tags that describe what this query is about.
Reply with one tag per line. Tags must be lowercase, no spaces, single-word
or hyphenated (examples: "testing", "deployment", "rust"). No prose, no
numbering, no punctuation other than hyphens.

Query: %s`

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
	if len(entries) == 0 {
		return nil, nil
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	queryTokens := tokenize(userQuery)
	hintTags := map[string]bool{}
	for _, t := range opts.HintTags {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" {
			hintTags[t] = true
		}
	}

	type scored struct {
		entry Entry
		score int
	}
	candidates := make([]scored, 0, len(entries))
	for _, e := range entries {
		if !e.ExpiresAt.IsZero() && !e.ExpiresAt.After(now) {
			continue
		}
		if opts.Exclude[e.Path] {
			continue
		}
		s := e.Priority
		for _, t := range e.Tags {
			if hintTags[t] {
				s += 2
			}
			if queryTokens[t] {
				s++
			}
		}
		nameToks := tokenize(e.Name)
		for tok := range queryTokens {
			if nameToks[tok] {
				s++
			}
		}
		candidates = append(candidates, scored{entry: e, score: s})
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].entry.Modified.After(candidates[j].entry.Modified)
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	out := make([]Record, 0, len(candidates))
	for _, c := range candidates {
		body, _ := Body(c.entry.Path)
		out = append(out, Record{Entry: c.entry, Body: body})
	}
	return out, nil
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
		Model:       model,
		System:      "You extract short, lowercase, hyphenated keyword tags from a question. No prose, one tag per line, at most three lines.",
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
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func parseTagLines(s string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(s, "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// strip common list prefixes models still emit
		raw = strings.TrimPrefix(raw, "- ")
		raw = strings.TrimPrefix(raw, "* ")
		raw = strings.Trim(raw, "`\"")
		raw = strings.ToLower(raw)
		if !tagPattern.MatchString(raw) {
			continue
		}
		if seen[raw] {
			continue
		}
		seen[raw] = true
		out = append(out, raw)
		if len(out) == 3 {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// tokenize splits s on non-word boundaries, lowercases, and drops short
// noise tokens. used by the deterministic phase-1 scorer.
func tokenize(s string) map[string]bool {
	out := map[string]bool{}
	cur := strings.Builder{}
	flush := func() {
		t := strings.ToLower(cur.String())
		cur.Reset()
		if len(t) < 3 {
			return
		}
		out[t] = true
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			cur.WriteRune(r)
		case r == '-' || r == '_':
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return out
}
