package wire

import (
	"regexp"
	"strings"
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
	end := 0
	for end < len(s) {
		c := s[end]
		isIdent := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-'
		if !isIdent {
			break
		}
		end++
	}
	return s[:end]
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
	s = bareDSMLRe.ReplaceAllString(s, "")
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
		s = bareDSMLRe.ReplaceAllString(s, "")
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
func repairToolArgs(raw []byte) ([]byte, bool) {
	// trim leading prose: keep from the first '{' or '['.
	start := -1
	for i, b := range raw {
		if b == '{' || b == '[' {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, false
	}
	s := raw[start:]
	// walk to find the matched closing brace honoring string literals + escapes,
	// then drop any trailing junk past it.
	depth := 0
	inStr := false
	esc := false
	end := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				end = i + 1
				break
			}
			if depth < 0 {
				return nil, false
			}
		}
		if end > 0 {
			break
		}
	}
	if end > 0 {
		// case 1: balanced object found; drop trailing junk.
		return s[:end], true
	}
	// case 2: unterminated. close it by appending missing braces/brackets.
	// re-scan to count unmatched opens.
	opens := 0
	stack := make([]byte, 0, 8)
	inStr = false
	esc = false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			stack = append(stack, '}')
			opens++
		case '[':
			stack = append(stack, ']')
			opens++
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if inStr {
		// unterminated string — close it then the containers.
		s = append(s, '"')
	}
	if len(stack) == 0 {
		return nil, false
	}
	// close opens in reverse.
	for i := len(stack) - 1; i >= 0; i-- {
		s = append(s, stack[i])
	}
	return s, opens > 0
}

// truncForErr clips raw bytes for embedding in error messages.
func truncForErr(b []byte) string {
	const max = 160
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}
