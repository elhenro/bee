package llm

import (
	"cmp"
	"regexp"
	"strings"
)

// attrEnvelopeRe matches `<NAME k=v k="v" k='v' ...>` (with or without
// self-close `/>`). Captures the tool-name and the raw attribute span so the
// rewriter can convert attrs into a JSON args object. Tool-name shape mirrors
// openTagRe (snake/lowercase with optional hyphens). The attribute span is
// permissive: anything up to the closing `>` that isn't another `<`.
var attrEnvelopeRe = regexp.MustCompile(`(?is)<([a-z_][a-z0-9_\-]*)((?:\s+[a-z_][a-z0-9_\-]*\s*=\s*(?:"[^"]*"|'[^']*'|[^\s/>]+))+)\s*(/?)>`)

// attrPairRe captures one key/value attribute. Values may be double-quoted,
// single-quoted, or bare (no whitespace).
var attrPairRe = regexp.MustCompile(`(?is)([a-z_][a-z0-9_\-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s/>]+))`)

// normalizeAttrEnvelopes rewrites `<NAME k="v" .../>` and `<NAME k="v">...</NAME>`
// into `<NAME>{"k":"v",...}</NAME>`. Only rewrites when the tag has at least one
// attribute (bare `<NAME>` is left for extractToolCalls). Body between explicit
// open/close is dropped. Only known tool names are rewritten, and never inside
// the `{...}` body of an opened `<known>{` call: that is JSON content
// (HTML/JSX with attributes) and must reach the arg parser untouched.
func normalizeAttrEnvelopes(s string, known map[string]bool) string {
	if !strings.Contains(s, "=") || len(known) == 0 {
		return s
	}
	protected := jsonBodyRanges(s, known)
	var b strings.Builder
	b.Grow(len(s) + 32)
	cur := 0
	for cur < len(s) {
		loc := attrEnvelopeRe.FindStringSubmatchIndex(s[cur:])
		if loc == nil {
			b.WriteString(s[cur:])
			break
		}
		matchStart := cur + loc[0]
		matchEnd := cur + loc[1]
		name := strings.ToLower(s[cur+loc[2] : cur+loc[3]])
		if !known[name] || inRanges(protected, matchStart) {
			b.WriteString(s[cur:matchEnd])
			cur = matchEnd
			continue
		}
		selfClose := loc[7] > loc[6]
		b.WriteString(s[cur:matchStart])
		b.WriteString(envelope(name, parseAttrPairs(s[cur+loc[4]:cur+loc[5]])))
		cur = matchEnd
		if selfClose {
			continue
		}
		// non-self-close: consume up to and including matching `</NAME>` so the
		// body doesn't double-emit. Missing close is tolerated — body discarded.
		if cl := foldLoc(s[cur:], "</"+name+">"); cl != nil {
			cur += cl[1]
		}
	}
	return b.String()
}

// jsonBodyRanges returns [start,end) spans of the JSON object following each
// `<known>` open tag. An unbalanced body (stream cut) runs to the end of s.
func jsonBodyRanges(s string, known map[string]bool) [][2]int {
	var out [][2]int
	cur := 0
	for cur < len(s) {
		loc := openTagRe.FindStringSubmatchIndex(s[cur:])
		if loc == nil {
			break
		}
		tagEnd := cur + loc[1]
		name := strings.ToLower(s[cur+loc[2] : cur+loc[3]])
		cur = tagEnd
		if !known[name] {
			continue
		}
		i := skipSpace(s, tagEnd)
		if i >= len(s) || s[i] != '{' {
			continue
		}
		end := len(s)
		if m := matchBraces(s, i); m >= 0 {
			end = m + 1
		}
		out = append(out, [2]int{i, end})
		cur = end
	}
	return out
}

func inRanges(ranges [][2]int, pos int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}

// parseAttrPairs walks an HTML-style attribute span and returns a key→value
// map. Values are decoded via decodeHermesScalar so `count=3` → number,
// `enabled="true"` → bool, everything else → string.
func parseAttrPairs(span string) map[string]any {
	pairs := attrPairRe.FindAllStringSubmatch(span, -1)
	args := make(map[string]any, len(pairs))
	for _, p := range pairs {
		args[p[1]] = decodeHermesScalar(cmp.Or(p[2], p[3], p[4]))
	}
	return args
}
