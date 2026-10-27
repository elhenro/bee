package wire

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// modelMarkupRe matches the DeepSeek / chat-template "special token" leaks
// that show up when a model trained for one tool-calling format is forced
// into another (notably deepseek-v4-flash emitting `<｜DSML｜invoke` and
// `</｜DSML｜parameter` into native openai tool_calls). The fullwidth bar
// `｜` (U+FF5C) is the marker; we strip from `<` (or `</`) up through the
// next `>` or end-of-string.
var modelMarkupRe = regexp.MustCompile(`</?\x{FF5C}[^<>]*(?:\x{FF5C}[^<>]*)*>?`)

// bareDSMLRe matches a bare special-token fragment delimited by fullwidth bars
// without surrounding `<...>` brackets — e.g. `｜DSML｜search` where the leading
// `<` was already stripped or never emitted. Tokens inside cannot contain `｜`,
// `<`, or `>`. Applied after modelMarkupRe to catch leaks that slipped past
// the bracketed pattern (notably deepseek-v4-flash emitting bare names).
var bareDSMLRe = regexp.MustCompile(`\x{FF5C}[^\x{FF5C}<>]*\x{FF5C}`)

// bareLeakRe is the conservative form of bareDSMLRe for argument payloads,
// which carry user content: only special-token names (DSML, tool_*,
// *_of_*) between bars match, so fullwidth bars used as separators in
// legit text (`ホーム｜会社概要｜お問い合わせ`) survive.
var bareLeakRe = regexp.MustCompile(`[ \t]*\x{FF5C}+(?:DSML|tool[\w\x{2581}]*|[\w\x{2581}]*_of_[\w\x{2581}]*|[\w]*\x{2581}[\w\x{2581}]*)\x{FF5C}+`)

// also catch dangling `</parameter>` / `</tool_call>` tags that some
// templates emit alongside the special-token wrapper. anchored to the
// end of input so we only strip TRAILING leak tags — never tags that
// appear inside a quoted string value (which would corrupt user content
// like `{"content":"foo</parameter>bar"}` or any file containing those
// literal tokens).
var stuckClosingTagRe = regexp.MustCompile(`(?:</(?:parameter|invoke|tool_call|tool_calls|function|name)\s*>\s*)+$`)

// SanitizeToolName extracts a clean identifier from a possibly-noisy tool
// name. Some models inject markup or extra fields into function.name,
// e.g. `"read path=\"/x\"</｜DSML｜parameter"`. Take the leading identifier
// run after trimming quotes/markup. Returns "" if nothing identifier-like
// is found (caller should surface a parse error).
func SanitizeToolName(raw string) string {
	s := strings.TrimSpace(raw)
	s = modelMarkupRe.ReplaceAllString(s, "")
	s = bareDSMLRe.ReplaceAllString(s, "")
	s = strings.TrimLeft(s, "\"' \t\r\n")
	if end := strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}); end >= 0 {
		s = s[:end]
	}
	return s
}

// StripMarkupBytes removes DSML / stray closing tags from a raw byte slice
// before JSON parsing. preserves length-on-success guarantees: nil in → nil
// out, empty in → empty out.
func StripMarkupBytes(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	s := string(b)
	s = modelMarkupRe.ReplaceAllString(s, "")
	s = bareLeakRe.ReplaceAllString(s, "")
	s = stuckClosingTagRe.ReplaceAllString(s, "")
	return []byte(s)
}

// StripMarkupInValues walks string values in a parsed args map and strips
// model markup tokens. Handles nested maps and slices. Mutates in place.
func StripMarkupInValues(m map[string]any) {
	for k, v := range m {
		m[k] = stripMarkupAny(v)
	}
}

func stripMarkupAny(v any) any {
	switch x := v.(type) {
	case string:
		// only strip the fullwidth-pipe leak token from decoded values.
		// NEVER strip `</parameter>` etc here — that's user content past
		// the JSON layer and must round-trip verbatim. trailing leak tags
		// are already removed pre-parse by StripMarkupBytes.
		s := modelMarkupRe.ReplaceAllString(x, "")
		s = bareLeakRe.ReplaceAllString(s, "")
		if s == x {
			return x
		}
		// whitespace that only separated the removed token from the value
		return strings.TrimRight(s, " \t\r\n")
	case map[string]any:
		StripMarkupInValues(x)
		return x
	case []any:
		for i, e := range x {
			x[i] = stripMarkupAny(e)
		}
		return x
	default:
		return v
	}
}

// repairToolArgs tries best-effort fixes for noisy model output that won't
// round-trip through json.Unmarshal. Targets the v4-flash failure modes seen
// in the wild:
//   - trailing junk after a balanced object: `{...}}` or `{...} extra`
//   - unterminated object: `{...` with missing `}`
//   - leading whitespace or stray prose before the first `{`
//
// Returns the repaired bytes and true on success; false when nothing
// recognizable was found (caller should surface the original error).
//
// closeOpen=false skips the unterminated case: a truncated stream must not have
// its cut-off object closed into something that parses.
func repairToolArgs(raw []byte, closeOpen bool) ([]byte, bool) {
	// trim leading prose: keep from the first '{' or '['.
	start := bytes.IndexAny(raw, "{[")
	if start < 0 {
		return nil, false
	}
	s := raw[start:]
	// walk honoring string literals; the first point where every opener is
	// closed ends the value, anything past it is junk.
	var stack []byte
	inStr := false
	for i, c := range OutsideStrings(s) {
		switch c {
		case '"':
			inStr = !inStr
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return s[:i+1], true
			}
		}
	}
	if !closeOpen {
		return nil, false
	}
	// unterminated: close an open string, then the containers in reverse.
	if inStr {
		s = append(s, '"')
	}
	for _, s0 := range slices.Backward(stack) {
		s = append(s, s0)
	}
	return s, true
}

// truncForErr clips raw bytes for embedding in error messages.
func truncForErr(b []byte) string {
	const max = 160
	if len(b) <= max {
		return string(b)
	}
	return types.CutBytes(string(b), max) + "…"
}
