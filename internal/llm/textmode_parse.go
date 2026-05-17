package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
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
			out.WriteString(s[cur : tagEnd])
			cur = tagEnd
			continue
		}
		// look for matching close tag (case-insensitive).
		closeRe := regexp.MustCompile(`(?i)</` + regexp.QuoteMeta(name) + `>`)
		closeLoc := closeRe.FindStringIndex(s[tagEnd:])
		out.WriteString(s[cur:tagStart])
		var body string
		var advance int
		if closeLoc == nil {
			body = s[tagEnd:]
			advance = len(s)
		} else {
			body = s[tagEnd : tagEnd+closeLoc[0]]
			advance = tagEnd + closeLoc[1]
		}
		input := parseToolArgs(body)
		calls = append(calls, parsedCall{Name: canonical[name], Input: input})
		cur = advance
	}
	cleaned := strings.TrimSpace(squeezeBlankLines(out.String()))
	return calls, cleaned
}

// parseToolArgs decodes the JSON-ish body of a tool tag. Tries strict JSON
// first, then a lenient repair pass; on total failure returns a map with
// `_parse_error` so the tool layer can surface the issue instead of running
// with empty args silently.
func parseToolArgs(body string) map[string]any {
	body = strings.TrimSpace(body)
	if body == "" {
		return map[string]any{}
	}
	body = stripCodeFence(body)
	body = string(wire.StripMarkupBytes([]byte(body)))
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err == nil {
		wire.StripMarkupInValues(v)
		return v
	}
	if repaired, ok := lenientJSONRepair(body); ok {
		if err := json.Unmarshal([]byte(repaired), &v); err == nil {
			wire.StripMarkupInValues(v)
			return v
		}
	}
	return map[string]any{
		"_parse_error": fmt.Sprintf("invalid JSON args: %s", truncate(body, 200)),
	}
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

// lenientJSONRepair handles the two failure modes seen most often from
// local models: a trailing comma inside an object/array, and raw newlines
// inside string values (model wrote a multiline content arg without
// escaping). Other errors fall through to caller's failure path.
func lenientJSONRepair(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	repaired := stripTrailingCommas(s)
	repaired = escapeBareNewlinesInStrings(repaired)
	if repaired == s {
		return "", false
	}
	return repaired, true
}

// stripTrailingCommas removes `,` immediately before `}` or `]`, honoring
// strings. Common from models that learned JS object syntax.
func stripTrailingCommas(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
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
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			if j < len(s) && (s[j] == '}' || s[j] == ']') {
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

// escapeBareNewlinesInStrings replaces raw \n/\r inside JSON string literals
// with `\n` escape sequences. Required because models often write tool args
// with literal newlines in `content` fields.
func escapeBareNewlinesInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStr {
			b.WriteByte(c)
			if c == '"' {
				inStr = true
			}
			continue
		}
		if esc {
			b.WriteByte(c)
			esc = false
			continue
		}
		switch c {
		case '\\':
			b.WriteByte(c)
			esc = true
		case '"':
			b.WriteByte(c)
			inStr = false
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
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
	return s[:n] + "..."
}
