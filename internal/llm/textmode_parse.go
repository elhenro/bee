package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
	"github.com/elhenro/bee/internal/types"
)

// parsedCall is one extracted tool invocation. Order preserved from text.
type parsedCall struct {
	Name  string
	Input map[string]any
}

// openTagRe matches an XML opening tag whose name is a valid tool ident.
// Tool names in bee are snake/lowercase but we accept hyphen too.
// Case-insensitive — the canonical name is resolved via the known-tools map.
var openTagRe = regexp.MustCompile(`(?i)<([a-z_][a-z0-9_\-]*)>`)

// extractToolCalls scans s for `<name>...</name>` blocks where name is in
// known. Returns the calls in source order and the text with those blocks
// removed (and surrounding whitespace squeezed). known is the lowercase
// tool-name set; canonical maps lower → original-cased name.
//
// Tolerant parsing: a missing close tag consumes the rest of the text as
// the body. JSON repair (trailing comma + bare newline in string) runs
// before giving up; final fallback emits the call with `_parse_error`.
func extractToolCalls(s string, known map[string]bool, canonical map[string]string) ([]parsedCall, string) {
	if len(known) == 0 || s == "" {
		return nil, s
	}
	var calls []parsedCall
	var out strings.Builder
	cur := 0
	for cur < len(s) {
		loc := openTagRe.FindStringSubmatchIndex(s[cur:])
		if loc == nil {
			out.WriteString(s[cur:])
			break
		}
		tagStart := cur + loc[0]
		tagEnd := cur + loc[1]
		name := strings.ToLower(s[cur+loc[2] : cur+loc[3]])
		if !known[name] {
			out.WriteString(s[cur:tagEnd])
			cur = tagEnd
			continue
		}
		out.WriteString(s[cur:tagStart])
		body, advance := s[tagEnd:], len(s)
		if closeLoc := foldLoc(body, "</"+name+">"); closeLoc != nil {
			body, advance = body[:closeLoc[0]], tagEnd+closeLoc[1]
		}
		calls = append(calls, parsedCall{Name: canonical[name], Input: parseToolArgs(body)})
		cur = advance
	}
	return calls, strings.TrimSpace(squeezeBlankLines(out.String()))
}

// parseToolArgs decodes the JSON-ish body of a tool tag. Tries strict JSON
// first, then a lenient repair pass; on total failure returns a map with
// `_parse_error` so the tool layer can surface the issue instead of running
// with empty args silently.
//
// Guard: if the lenient repair pass shrinks a `content` field by more than
// 25% vs the raw body length (rough proxy — content dominates `write`/`edit`
// payloads), surface `_parse_error` instead of writing mangled output.
// Repair heuristics (newline escaping + trailing-comma strip) can desync on
// model output that has both raw newlines AND unescaped quotes, producing
// technically-valid JSON with the wrong structure.
func parseToolArgs(body string) map[string]any {
	body = strings.TrimSpace(body)
	if body == "" {
		return map[string]any{}
	}
	body = stripCodeFence(body)
	raw := body // pre-markup-strip copy for the param-name-as-tag fallback
	// F2: hermes-style `<parameter=K>V</parameter>` children inside a real
	// tool tag. qwen3 mixes shapes — outer tag is the canonical tool name,
	// inner params follow hermes convention. detect BEFORE StripMarkupBytes
	// because that strips trailing `</parameter>` runs and destroys the last
	// param block.
	if strings.Contains(body, "<parameter=") {
		if args := hermesParams(body); len(args) > 0 {
			return args
		}
	}
	body = string(wire.StripMarkupBytes([]byte(body)))
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err == nil {
		wire.StripMarkupInValues(v)
		return v
	}
	if repaired, ok := lenientJSONRepair(body); ok {
		if err := json.Unmarshal([]byte(repaired), &v); err == nil {
			wire.StripMarkupInValues(v)
			// guard: large `write`/`edit` payloads where repaired content
			// is less than half the raw body length signal a likely
			// state-machine desync in the newline/quote repair pass.
			// 500-byte floor avoids flagging trivial cases where content
			// is legitimately tiny vs JSON structural overhead.
			if c, isStr := v["content"].(string); isStr {
				if len(body) >= 500 && len(c)*2 < len(body) {
					return map[string]any{
						"_parse_error": fmt.Sprintf("content arg shrank suspiciously after JSON repair (raw=%d bytes, content=%d bytes) — refusing to write potentially mangled output. Re-emit with escaped newlines and quotes.", len(body), len(c)),
					}
				}
			}
			return v
		}
	}
	// last resort: param-name-as-tag shape (`<path>..</path><old>..</old>..`)
	// some local models emit instead of JSON. run on the pre-strip body.
	if args := paramTagFallback(raw); args != nil {
		return args
	}
	return map[string]any{
		"_parse_error": fmt.Sprintf("invalid JSON args: %s", truncate(body, 200)),
	}
}

var (
	paramOpenRe   = regexp.MustCompile(`(?is)^\s*<([a-z_][a-z0-9_\-]*)>`)
	leakedCloseRe = regexp.MustCompile(`(?is)^\s*</[a-z_][a-z0-9_\-]*>?\s*$`)
)

// paramTagFallback parses the param-name-as-tag shape: each tool arg wrapped
// in its own `<KEY>VALUE</KEY>` block (e.g. an `edit` emitted as
// `<path>..</path><old>..</old><new>..</new>`). Distinct from the hermes
// `<parameter=KEY>` form handled above. Fires only when the whole body is
// nothing but such blocks plus whitespace and an optional leaked tool close
// tag (tolerated like `</edit`), so it can't misread a payload whose value
// legitimately contains markup. Returns nil when the shape doesn't match.
func paramTagFallback(body string) map[string]any {
	args := map[string]any{}
	rest := body
	for strings.TrimSpace(rest) != "" && !leakedCloseRe.MatchString(rest) {
		m := paramOpenRe.FindStringSubmatchIndex(rest)
		if m == nil {
			return nil // non-tag content present — not this shape
		}
		key := rest[m[2]:m[3]]
		after := rest[m[1]:]
		closeTag := "</" + key + ">"
		ci := indexFold(after, closeTag)
		if ci < 0 {
			return nil // unterminated block — bail rather than guess
		}
		args[key] = decodeTagValue(after[:ci])
		rest = after[ci+len(closeTag):]
	}
	if len(args) == 0 {
		return nil
	}
	return args
}

// indexFold is a case-insensitive strings.Index. Folding is ASCII only so
// byte offsets stay valid for non-ASCII text (full lowercasing can change a
// rune's byte length, e.g. İ).
func indexFold(s, sub string) int {
	return strings.Index(asciiLower(s), asciiLower(sub))
}

// foldLoc returns the [start,end) of the first case-insensitive sub in s, nil
// when absent.
func foldLoc(s, sub string) []int {
	i := indexFold(s, sub)
	if i < 0 {
		return nil
	}
	return []int{i, i + len(sub)}
}

// skipSpace returns the index of the first non-whitespace byte of s at or
// after i, len(s) when only whitespace remains.
func skipSpace(s string, i int) int {
	return len(s) - len(strings.TrimLeft(s[i:], " \t\r\n"))
}

// jsonValue decodes t when it opens like a JSON value; anything else stays
// text, so `ls -la` is never a parse error.
func jsonValue(t string) (any, bool) {
	if t == "" || !strings.ContainsRune(`{["tfn-0123456789`, rune(t[0])) {
		return nil, false
	}
	var out any
	if json.Unmarshal([]byte(t), &out) != nil {
		return nil, false
	}
	return out, true
}

// decodeHermesScalar turns a raw hermes parameter body into a Go value. Tries
// JSON number/bool/null first (so `3` → float64(3), `true` → true), then a
// fenced JSON object/array literal (so a parameter holding `{"a":1}` decodes
// nested), and finally falls back to the raw string. Surrounding ```...```
// fences some models add inside parameter bodies are dropped.
func decodeHermesScalar(v string) any {
	v = stripCodeFence(v)
	if out, ok := jsonValue(v); ok {
		return out
	}
	return v
}

// decodeTagValue extracts a param value, stripping only the wrapper newline
// the model puts right after `>` and before `</` — NOT interior indentation,
// which is semantic for whitespace-sensitive args like `edit` old/new. Decodes
// numeric/bool/quoted JSON scalars; everything else stays a literal string.
func decodeTagValue(v string) any {
	v = strings.TrimPrefix(v, "\r\n")
	v = strings.TrimPrefix(v, "\n")
	v = strings.TrimSuffix(v, "\r\n")
	v = strings.TrimSuffix(v, "\n")
	if out, ok := jsonValue(strings.TrimSpace(v)); ok {
		return out
	}
	return v
}

// stripCodeFence trims ```json fences some models add around the args.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[i+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

// squeezeBlankLines collapses runs of 3+ newlines down to 2 so removing
// tool blocks doesn't leave gaping gaps.
func squeezeBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return types.CutBytes(s, n) + "..."
}
