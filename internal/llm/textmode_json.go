package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/llm/wire"
)

// extractJSONToolCalls scans s for bare JSON objects that look like tool
// calls without the XML envelope. Many models (including big ones like
// GPT/Claude when textmode is forced) revert to their native JSON
// function-call shape. Recognized:
//
//	{"type":"<tool>",...rest as args}                 // caveman / bee transcript
//	{"name":"<tool>","arguments":{...}}               // OpenAI-ish
//	{"name":"<tool>","input":{...}}                   // Anthropic-ish
//	{"type":"function","function":{"name":"<tool>","arguments":<obj|string>}}
//
// Only objects resolving to a name in known are accepted. Returns the calls
// in source order and the text with those JSON blocks removed.
func extractJSONToolCalls(s string, known map[string]bool, canonical map[string]string) ([]parsedCall, string) {
	if len(known) == 0 || s == "" {
		return nil, s
	}
	var calls []parsedCall
	var out strings.Builder
	cur := 0
	for cur < len(s) {
		rel := strings.IndexByte(s[cur:], '{')
		if rel < 0 {
			out.WriteString(s[cur:])
			break
		}
		open := cur + rel
		end := matchBraces(s, open)
		if end < 0 {
			out.WriteString(s[cur:])
			break
		}
		call, ok := parseJSONToolCall(s[open:end+1], known, canonical)
		if !ok {
			// not a tool call — keep up to and including this `{`, continue past it
			out.WriteString(s[cur : open+1])
			cur = open + 1
			continue
		}
		// strip the matched JSON; also drop a wrapping ```json fence if present
		prefix := s[cur:open]
		trimmedPrefix := strings.TrimRight(prefix, " \t\r\n")
		switch {
		case strings.HasSuffix(trimmedPrefix, "```json"):
			prefix = strings.TrimSuffix(trimmedPrefix, "```json")
		case strings.HasSuffix(trimmedPrefix, "```"):
			prefix = strings.TrimSuffix(trimmedPrefix, "```")
		}
		out.WriteString(prefix)
		calls = append(calls, call)
		cur = end + 1
		// skip immediately following closing fence (after optional whitespace)
		rest := s[cur:]
		if after := strings.TrimLeft(rest, " \t\r\n"); strings.HasPrefix(after, "```") {
			cur += len(rest) - len(after) + 3
		}
	}
	return calls, strings.TrimSpace(squeezeBlankLines(out.String()))
}

// matchBraces returns the index of the `}` that closes the `{` at start,
// respecting JSON string literals. -1 if unbalanced.
func matchBraces(s string, start int) int {
	if start >= len(s) || s[start] != '{' {
		return -1
	}
	depth := 0
	for i, c := range wire.OutsideStrings(s[start:]) {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return start + i
			}
		}
	}
	return -1
}

// resolveToolName maps a model-supplied name to the canonical tool name.
func resolveToolName(name string, known map[string]bool, canonical map[string]string) (string, bool) {
	low := strings.ToLower(strings.TrimSpace(name))
	return canonical[low], low != "" && known[low]
}

// parseJSONToolCall recognizes the documented JSON tool-call shapes. Returns
// ok=false if body is not a tool call (caller keeps text verbatim).
func parseJSONToolCall(body string, known map[string]bool, canonical map[string]string) (parsedCall, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		// try lenient repair for trailing commas / unescaped newlines
		repaired, ok := lenientJSONRepair(body)
		if !ok || json.Unmarshal([]byte(repaired), &raw) != nil {
			return parsedCall{}, false
		}
	}
	// OpenAI raw: {"type":"function","function":{...}}
	if t, _ := raw["type"].(string); t == "function" {
		fn, ok := raw["function"].(map[string]any)
		if !ok {
			return parsedCall{}, false
		}
		name, _ := fn["name"].(string)
		canon, ok := resolveToolName(name, known, canonical)
		if !ok {
			return parsedCall{}, false
		}
		args := map[string]any{}
		switch a := fn["arguments"].(type) {
		case map[string]any:
			args = a
		case string:
			args = stringArgs(a)
		}
		return parsedCall{Name: canon, Input: args}, true
	}
	// {"name":"<tool>",...} and {"type":"<tool>",...}
	for _, key := range []string{"name", "type"} {
		if name, ok := raw[key].(string); ok {
			if canon, ok := resolveToolName(name, known, canonical); ok {
				return parsedCall{Name: canon, Input: argsFromRaw(raw, key)}, true
			}
		}
	}
	return parsedCall{}, false
}

// stringArgs decodes tool args carried as a JSON string. Malformed input comes
// back as a _parse_error instead of empty args, so the tool layer nudges the
// model to re-emit rather than running mangled input.
func stringArgs(a string) map[string]any {
	var v map[string]any
	if err := json.Unmarshal([]byte(a), &v); err != nil {
		return map[string]any{"_parse_error": fmt.Sprintf("invalid JSON arguments: %s", truncate(a, 200)), "_raw_args": a}
	}
	return v
}

// argsFromRaw extracts the args object out of a generic tool-call envelope.
// Prefers `arguments` or `input` when present; otherwise treats every other
// top-level key as an inlined arg.
func argsFromRaw(raw map[string]any, nameKey string) map[string]any {
	if a, ok := raw["arguments"].(map[string]any); ok {
		return a
	}
	if a, ok := raw["input"].(map[string]any); ok {
		return a
	}
	if as, ok := raw["arguments"].(string); ok {
		return stringArgs(as)
	}
	out := make(map[string]any, len(raw))
	for k, v := range raw {
		if k == nameKey || k == "arguments" || k == "input" {
			continue
		}
		out[k] = v
	}
	return out
}

// lenientJSONRepair handles the most common failure modes from local
// models: trailing comma, raw newlines inside strings, invalid backslash
// escapes (e.g. shell-regex `\|` or `\(` in a bash command), and a
// trailing unbalanced brace from envelope-leak truncation. Other errors
// fall through to caller's failure path.
func lenientJSONRepair(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	repaired := trimUnbalancedTail(stripTrailingCommas(escapeInStrings(s)))
	if repaired == s {
		return "", false
	}
	return repaired, true
}

// escapeInStrings repairs the inside of JSON string literals. A backslash
// before a char that is not a valid JSON escape (b, f, n, r, t, ", \, /, u) is
// doubled so JSON parses it as a literal `\X`: catches bash commands with grep
// regex alternation (`\|`) or group syntax (`\(`, `\)`). Raw newline, carriage
// return and tab, which models write into `content` fields, become escapes.
func escapeInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case !inStr:
			inStr = c == '"'
		case c == '"':
			inStr = false
		case c == '\\' && i+1 < len(s):
			if strings.IndexByte(`bfnrt"\/u`, s[i+1]) >= 0 {
				b.WriteByte(c)
				c = s[i+1]
				i++
			} else {
				// invalid escape: double the backslash, next char is plain
				b.WriteByte(c)
			}
		case c == '\n':
			b.WriteString(`\n`)
			continue
		case c == '\r':
			b.WriteString(`\r`)
			continue
		case c == '\t':
			b.WriteString(`\t`)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// stripTrailingCommas removes `,` immediately before `}` or `]`, honoring
// strings. Common from models that learned JS object syntax.
func stripTrailingCommas(s string) string {
	var b strings.Builder
	last := 0
	for i, c := range wire.OutsideStrings(s) {
		if c != ',' {
			continue
		}
		next := strings.TrimLeft(s[i+1:], " \t\n\r")
		if next != "" && (next[0] == '}' || next[0] == ']') {
			b.WriteString(s[last:i])
			last = i + 1
		}
	}
	b.WriteString(s[last:])
	return b.String()
}

// trimUnbalancedTail handles envelope truncation: model hit the `</tool>`
// stop sequence mid-emission, leaving a trailing unbalanced `}` or extra
// closing brace past the real end of the JSON object. Returns the longest
// prefix where brace depth returns to zero, dropping non-whitespace garbage
// past it. If no such prefix exists, returns the input unchanged.
//
// Conservative: only trims trailing garbage, never modifies the parsed
// region. A genuinely unterminated JSON ({"a":"b" with no close) is left
// alone so the caller's error path surfaces the real issue.
func trimUnbalancedTail(s string) string {
	depth, lastBalanced := 0, -1
	for i, c := range wire.OutsideStrings(s) {
		switch c {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				lastBalanced = i
			}
		}
	}
	if lastBalanced < 0 || strings.Trim(s[lastBalanced+1:], " \t\n\r") == "" {
		return s
	}
	return s[:lastBalanced+1]
}
