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

// hermesToolCallRe captures the qwen3 / hermes chat-template wrapper:
// `<tool_call>...body...</tool_call>`. Body is usually JSON but may be the
// `<function=NAME>...<parameter=...>` xml variant. Pre-processed away before
// the standard XML/JSON extractors run.
var hermesToolCallRe = regexp.MustCompile(`(?is)<tool_call>\s*(.*?)\s*</tool_call>`)

// toolNameTagRe matches `<tool_name>NAME</tool_name>` — a chat-template
// variant where the literal `tool_name` tag wraps the actual tool name and
// args follow as a bare JSON object. Seen from qwen3-A3B reading the textmode
// advert's `<tool_name>{...}</tool_name>` example too literally and treating
// `tool_name` as the tag instead of a placeholder. Normalized to the canonical
// `<NAME>{json}</NAME>` shape by normalizeHermesEnvelopes pass 1.5.
var toolNameTagRe = regexp.MustCompile(`(?is)<tool_name>\s*([a-z_][a-z0-9_\-]*)\s*</tool_name>`)

// hermesFunctionOpenRe matches the qwen3 xml variant opening tag:
// `<function=NAME>`. The matching close is searched procedurally so we can
// accept BOTH the literal `</function>` and the qwen3-in-the-wild `</NAME>`
// form (model echoes function name as close instead of the literal). Also
// tolerates missing close: the textmode stop-sequence list (`</NAME>`) cuts
// the stream the moment the model emits the name-form close, which means we
// never see `</function>` or `</tool_call>` past it.
var hermesFunctionOpenRe = regexp.MustCompile(`(?is)<function=([a-z_][a-z0-9_\-]*)>`)

// hermesFunctionCloseRe is the literal `</function>` close, used alongside
// the dynamically-built `</NAME>` close in normalizeHermesEnvelopes.
var hermesFunctionCloseRe = regexp.MustCompile(`(?is)</function>`)

// hermesParamRe captures one `<parameter=KEY>VALUE</parameter>` block. Used
// from inside hermesFunctionRe match bodies.
var hermesParamRe = regexp.MustCompile(`(?is)<parameter=([a-z_][a-z0-9_\-]*)>\s*(.*?)\s*</parameter>`)

// DSML envelope (special-token tool-call format used by some MoE models):
//
//	<｜｜DSML｜｜tool_calls>
//	  <｜｜DSML｜｜invoke name="bash">
//	    <｜｜DSML｜｜parameter name="command" string="true">grep ...</｜｜DSML｜｜parameter>
//	  </｜｜DSML｜｜invoke>
//	</｜｜DSML｜｜tool_calls>
//
// Pipes are fullwidth `｜` (U+FF5C) and counts vary by tokenizer (single,
// double, triple) so `[｜]+` covers all observed variants. Bee was previously
// stripping these as markup leak from native tool_call fields; the envelope
// also appears as raw content text, where it needs parsing not stripping —
// rewritten into canonical `<NAME>{json}</NAME>` so the standard extractor
// handles dispatch.
var dsmlInvokeOpenRe = regexp.MustCompile(`(?is)<\x{FF5C}+\s*DSML\s*\x{FF5C}+\s*invoke\s+name\s*=\s*["']([a-z_][a-z0-9_\-]*)["']\s*>`)
var dsmlInvokeCloseRe = regexp.MustCompile(`(?is)</\s*\x{FF5C}+\s*DSML\s*\x{FF5C}+\s*invoke\s*>`)
var dsmlToolCallsWrapperRe = regexp.MustCompile(`(?is)</?\s*\x{FF5C}+\s*DSML\s*\x{FF5C}+\s*tool_calls\s*>`)
var dsmlParamRe = regexp.MustCompile(`(?is)<\x{FF5C}+\s*DSML\s*\x{FF5C}+\s*parameter\s+name\s*=\s*["']([a-z_][a-z0-9_\-]*)["'](?:\s+string\s*=\s*["'](?:true|false)["'])?\s*>\s*(.*?)\s*</\s*\x{FF5C}+\s*DSML\s*\x{FF5C}+\s*parameter\s*>`)

// normalizeHermesEnvelopes rewrites qwen3 / hermes-style tool-call wrappers
// into shapes the existing XML / JSON extractors already handle. Specifically:
//
//   - `<tool_call>{json}</tool_call>` → `{json}` (extractJSONToolCalls picks
//     up the bare JSON envelope)
//   - `<tool_call><function=NAME><parameter=K>V</parameter>...</function></tool_call>`
//     and the un-wrapped `<function=NAME>...</function>` form → synthesized
//     `<NAME>{"K":"V",...}</NAME>` so extractToolCalls handles it natively
//
// Why pre-process instead of adding a third extractor: keeps the call-site in
// relay() unchanged and avoids fighting brace-matching on the
// `<function=NAME>` form where the body isn't valid JSON at all.
func normalizeHermesEnvelopes(s string) string {
	// pass 0: rewrite attribute-style `<NAME k="v" k="v"/>` (and explicit
	// close form `<NAME k="v">...</NAME>`) into canonical
	// `<NAME>{"k":"v",...}</NAME>`. catches the HTML/XML-flavored envelope
	// some models reach for when the JSON-in-body form doesn't stick. tool
	// name resolution is deferred to extractToolCalls.
	s = normalizeAttrEnvelopes(s)
	// pass 1: unwrap <tool_call>...</tool_call> envelopes. body re-emitted
	// inline so a json body becomes bare json, and a <function=...> body
	// becomes a bare <function=...> match for pass 2. tolerate missing
	// </tool_call> (stop sequence often cuts the stream before it lands).
	s = unwrapToolCallEnvelopes(s)
	// pass 1.2: rewrite DSML invoke/parameter envelopes into <NAME>{json}</NAME>.
	// wrapper <｜｜DSML｜｜tool_calls> is dropped; each nested invoke becomes one
	// canonical envelope.
	s = normalizeDSMLEnvelopes(s)
	// pass 1.5: rewrite <tool_name>NAME</tool_name>{json} → <NAME>{json}</NAME>.
	// some templates emit the tool name as the content of a literal <tool_name>
	// tag (qwen3-A3B reading the advert example too literally), then drop a
	// bare JSON args object right after. consume any whitespace + a balanced
	// `{...}` block following the close so the synthesized envelope wraps the
	// args. when no `{` follows, synthesize an empty-args call so the loop at
	// least advances instead of hanging.
	s = normalizeToolNameVariant(s)
	// pass 2: rewrite <function=NAME>...<parameter=K>V</parameter>... into
	// <NAME>{"K":"V",...}</NAME>. close tag is either literal </function>
	// (hermes/openai chat-template) or </NAME> (qwen3 in the wild), or
	// missing entirely (textmode stop-seq cut the stream). last-resort rest-
	// of-buffer consume is what unblocks the loop when the model emits the
	// name-form close and bee's stop list trims everything past it.
	var b strings.Builder
	b.Grow(len(s))
	cur := 0
	for cur < len(s) {
		loc := hermesFunctionOpenRe.FindStringSubmatchIndex(s[cur:])
		if loc == nil {
			b.WriteString(s[cur:])
			break
		}
		openStart := cur + loc[0]
		openEnd := cur + loc[1]
		name := strings.ToLower(s[cur+loc[2] : cur+loc[3]])
		tail := s[openEnd:]
		bodyEnd, advance := findHermesClose(tail, name)
		body := tail[:bodyEnd]
		params := hermesParamRe.FindAllStringSubmatch(body, -1)
		args := make(map[string]any, len(params))
		for _, p := range params {
			if len(p) != 3 {
				continue
			}
			// hermes parameter bodies are raw scalars (numbers/strings) not
			// quoted JSON. decodeHermesScalar tries numeric/bool first so
			// `<parameter=n>3</parameter>` becomes a number, else string.
			args[p[1]] = decodeHermesScalar(strings.TrimSpace(p[2]))
		}
		buf, err := json.Marshal(args)
		if err != nil {
			b.WriteString(s[openStart : openEnd+advance])
			cur = openEnd + advance
			continue
		}
		b.WriteString(s[cur:openStart])
		b.WriteString("<" + name + ">" + string(buf) + "</" + name + ">")
		cur = openEnd + advance
	}
	return b.String()
}

// normalizeDSMLEnvelopes turns DSML invoke blocks into the canonical
// `<NAME>{json}</NAME>` shape and drops the surrounding
// `<｜｜DSML｜｜tool_calls>` wrapper. Each `<｜｜DSML｜｜parameter name="K" …>V</…>`
// inside an invoke becomes one JSON key. Missing `</invoke>` close is
// tolerated (stop sequence cuts the stream past the first param close): we
// consume to the next invoke open OR end of buffer. Re-running this on
// already-normalized text is a no-op since the invoke regex won't match the
// rewritten envelope.
func normalizeDSMLEnvelopes(s string) string {
	if !strings.Contains(s, "DSML") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	cur := 0
	for cur < len(s) {
		loc := dsmlInvokeOpenRe.FindStringSubmatchIndex(s[cur:])
		if loc == nil {
			b.WriteString(s[cur:])
			break
		}
		openStart := cur + loc[0]
		openEnd := cur + loc[1]
		name := strings.ToLower(s[cur+loc[2] : cur+loc[3]])
		tail := s[openEnd:]
		// look for </…DSML…invoke> close OR the next <…DSML…invoke … > open
		// (model emitted two calls without closing the first — rare but
		// observed). Whichever comes first ends the body. Missing both →
		// consume rest of buffer.
		var bodyEnd, advance int
		ci := dsmlInvokeCloseRe.FindStringIndex(tail)
		ni := dsmlInvokeOpenRe.FindStringIndex(tail)
		switch {
		case ci == nil && ni == nil:
			bodyEnd = len(tail)
			advance = len(tail)
		case ci == nil:
			bodyEnd = ni[0]
			advance = ni[0]
		case ni == nil:
			bodyEnd = ci[0]
			advance = ci[1]
		default:
			if ci[0] <= ni[0] {
				bodyEnd, advance = ci[0], ci[1]
			} else {
				bodyEnd, advance = ni[0], ni[0]
			}
		}
		body := tail[:bodyEnd]
		params := dsmlParamRe.FindAllStringSubmatch(body, -1)
		args := make(map[string]any, len(params))
		for _, p := range params {
			if len(p) != 3 {
				continue
			}
			// DSML param values are always raw strings (string="true" attr is
			// just a type hint, value still arrives as text). Try scalar
			// decode for parity with hermes params so `{"n":3}` → number,
			// else keep verbatim.
			args[p[1]] = decodeHermesScalar(strings.TrimSpace(p[2]))
		}
		buf, err := json.Marshal(args)
		if err != nil {
			b.WriteString(s[openStart : openEnd+advance])
			cur = openEnd + advance
			continue
		}
		b.WriteString(s[cur:openStart])
		b.WriteString("<" + name + ">" + string(buf) + "</" + name + ">")
		cur = openEnd + advance
	}
	// drop tool_calls wrappers wherever they remain.
	return dsmlToolCallsWrapperRe.ReplaceAllString(b.String(), "")
}

// normalizeToolNameVariant rewrites `<tool_name>NAME</tool_name>{json}` into
// `<NAME>{json}</NAME>`. When no `{` follows the close tag, emits an empty-args
// envelope so the call name still surfaces. Args block is matched via brace
// counting to handle nested objects.
func normalizeToolNameVariant(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	cur := 0
	for cur < len(s) {
		loc := toolNameTagRe.FindStringSubmatchIndex(s[cur:])
		if loc == nil {
			b.WriteString(s[cur:])
			break
		}
		matchStart := cur + loc[0]
		matchEnd := cur + loc[1]
		name := strings.ToLower(s[cur+loc[2] : cur+loc[3]])
		b.WriteString(s[cur:matchStart])
		// look ahead for bare {json} args following optional whitespace.
		tail := s[matchEnd:]
		i := 0
		for i < len(tail) && (tail[i] == ' ' || tail[i] == '\t' || tail[i] == '\r' || tail[i] == '\n') {
			i++
		}
		if i >= len(tail) || tail[i] != '{' {
			b.WriteString("<" + name + ">{}</" + name + ">")
			cur = matchEnd + i
			continue
		}
		end := matchBraces(tail, i)
		if end < 0 {
			// unbalanced — assume rest of buffer is args body (stop-seq cut).
			b.WriteString("<" + name + ">" + tail[i:] + "}</" + name + ">")
			break
		}
		argsJSON := tail[i : end+1]
		b.WriteString("<" + name + ">" + argsJSON + "</" + name + ">")
		cur = matchEnd + end + 1
	}
	return b.String()
}

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
// open/close is preserved appended after the synthesized JSON — rare, but lets
// `<write path="x">content body</write>` round-trip when a model mixes shapes.
// Tool name validity is checked downstream; this pass is purely syntactic.
func normalizeAttrEnvelopes(s string) string {
	if !strings.Contains(s, "=") {
		return s
	}
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
		attrSpan := s[cur+loc[4] : cur+loc[5]]
		selfClose := loc[6] >= 0 && (cur+loc[7]) > (cur+loc[6]) && s[cur+loc[6]] == '/'

		args := parseAttrPairs(attrSpan)
		buf, err := json.Marshal(args)
		if err != nil {
			b.WriteString(s[cur:matchEnd])
			cur = matchEnd
			continue
		}
		b.WriteString(s[cur:matchStart])
		b.WriteString("<" + name + ">" + string(buf) + "</" + name + ">")
		cur = matchEnd
		if selfClose {
			continue
		}
		// non-self-close: consume up to and including matching `</NAME>` so the
		// body doesn't double-emit. Missing close is tolerated — body discarded.
		closeRe := regexp.MustCompile(`(?is)</` + regexp.QuoteMeta(name) + `>`)
		if cl := closeRe.FindStringIndex(s[cur:]); cl != nil {
			cur += cl[1]
		}
	}
	return b.String()
}

// parseAttrPairs walks an HTML-style attribute span and returns a key→value
// map. Values are decoded via decodeHermesScalar so `count=3` → number,
// `enabled="true"` → bool, everything else → string.
func parseAttrPairs(span string) map[string]any {
	pairs := attrPairRe.FindAllStringSubmatch(span, -1)
	args := make(map[string]any, len(pairs))
	for _, p := range pairs {
		if len(p) < 5 {
			continue
		}
		key := p[1]
		val := p[2]
		if val == "" {
			val = p[3]
		}
		if val == "" {
			val = p[4]
		}
		args[key] = decodeHermesScalar(val)
	}
	return args
}

// unwrapToolCallEnvelopes replaces <tool_call>BODY</tool_call> with BODY, AND
// also strips a dangling `<tool_call>` opener with no close — happens when the
// textmode stop sequence (`</NAME>`) terminates the stream past the function
// body but before the model emits </tool_call>.
func unwrapToolCallEnvelopes(s string) string {
	s = hermesToolCallRe.ReplaceAllString(s, "$1")
	// dangling open with no matching close — strip just the opener.
	s = strings.ReplaceAll(s, "<tool_call>", "")
	return s
}

// findHermesClose returns (body-end, advance-past-close) for a <function=NAME>
// body, accepting either </function> or </NAME> as close. Missing close is
// tolerated: body consumes rest of buffer, advance = len(tail). Whichever
// close appears FIRST wins so a body that contains a literal "</function>"
// followed later by a real "</NAME>" doesn't accidentally swallow further
// content.
func findHermesClose(tail, name string) (bodyEnd, advance int) {
	fi := hermesFunctionCloseRe.FindStringIndex(tail)
	closeNameRe := regexp.MustCompile(`(?is)</` + regexp.QuoteMeta(name) + `>`)
	ni := closeNameRe.FindStringIndex(tail)
	switch {
	case fi == nil && ni == nil:
		return len(tail), len(tail)
	case fi == nil:
		return ni[0], ni[1]
	case ni == nil:
		return fi[0], fi[1]
	default:
		if fi[0] <= ni[0] {
			return fi[0], fi[1]
		}
		return ni[0], ni[1]
	}
}

// decodeHermesScalar turns a raw hermes parameter body into a Go value. Tries
// JSON number/bool/null first (so `3` → float64(3), `true` → true), then a
// fenced JSON object/array literal (so a parameter holding `{"a":1}` decodes
// nested), and finally falls back to the raw string.
func decodeHermesScalar(v string) any {
	if v == "" {
		return ""
	}
	// strip surrounding ```...``` fences some models add inside parameter bodies.
	v = stripCodeFence(v)
	if v == "" {
		return ""
	}
	// only attempt JSON decode for shapes that look like JSON to avoid
	// turning `command: ls -la` into a parse error.
	c := v[0]
	if c == '{' || c == '[' || c == '"' || c == 't' || c == 'f' || c == 'n' || (c >= '0' && c <= '9') || c == '-' {
		var out any
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out
		}
	}
	return v
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
	// F2: hermes-style `<parameter=K>V</parameter>` children inside a real
	// tool tag. qwen3 mixes shapes — outer tag is the canonical tool name,
	// inner params follow hermes convention. detect BEFORE StripMarkupBytes
	// because that strips trailing `</parameter>` runs and destroys the last
	// param block.
	if strings.Contains(body, "<parameter=") {
		params := hermesParamRe.FindAllStringSubmatch(body, -1)
		if len(params) > 0 {
			args := make(map[string]any, len(params))
			for _, p := range params {
				if len(p) != 3 {
					continue
				}
				args[p[1]] = decodeHermesScalar(strings.TrimSpace(p[2]))
			}
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

// lenientJSONRepair handles the most common failure modes from local
// models: trailing comma, raw newlines inside strings, invalid backslash
// escapes (e.g. shell-regex `\|` or `\(` in a bash command), and a
// trailing unbalanced brace from envelope-leak truncation. Other errors
// fall through to caller's failure path.
//
// Order matters: escape-fix runs before newline-fix because a fixed
// `\|` lengthens the string by one char (`\\|`) and could shift any
// later byte-offset based pass; newline-fix is byte-walking so it's
// resilient to length changes. Trailing-comma runs last because it
// only looks at boundary chars.
func lenientJSONRepair(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	repaired := escapeInvalidBackslashesInStrings(s)
	repaired = escapeBareNewlinesInStrings(repaired)
	repaired = stripTrailingCommas(repaired)
	repaired = trimUnbalancedTail(repaired)
	if repaired == s {
		return "", false
	}
	return repaired, true
}

// escapeInvalidBackslashesInStrings finds `\X` inside JSON string literals
// where X is not a valid JSON escape (b, f, n, r, t, ", \, /, u) and
// doubles the backslash so JSON parses it as literal `\X`. Catches the
// common case of bash commands with grep regex alternation (`\|`) or
// regex group syntax (`\(`, `\)`) emitted directly into a `command` arg.
func escapeInvalidBackslashesInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStr {
			b.WriteByte(c)
			if c == '"' {
				inStr = true
			}
			continue
		}
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case 'b', 'f', 'n', 'r', 't', '"', '\\', '/', 'u':
				b.WriteByte(c)
				b.WriteByte(next)
				i++
				continue
			}
			// invalid escape — double the backslash.
			b.WriteString(`\\`)
			b.WriteByte(next)
			i++
			continue
		}
		b.WriteByte(c)
		if c == '"' {
			inStr = false
		}
	}
	return b.String()
}

// trimUnbalancedTail handles envelope truncation: model hit the `</tool>`
// stop sequence mid-emission, leaving a trailing unbalanced `}` or extra
// closing brace past the real end of the JSON object. Walks the prefix
// counting brace depth; returns the longest prefix where depth returns
// to zero. If no such prefix exists, returns the input unchanged.
//
// Conservative: only trims trailing garbage, never modifies the parsed
// region. A genuinely unterminated JSON ({"a":"b" with no close) is left
// alone so the caller's error path surfaces the real issue.
func trimUnbalancedTail(s string) string {
	depth := 0
	inStr := false
	esc := false
	lastBalanced := -1
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
				lastBalanced = i
			}
		}
	}
	if lastBalanced < 0 || lastBalanced == len(s)-1 {
		return s
	}
	// any non-whitespace past the balanced point → trim.
	for i := lastBalanced + 1; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			continue
		}
		return s[:lastBalanced+1]
	}
	return s
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
		body := s[open : end+1]
		call, ok := parseJSONToolCall(body, known, canonical)
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
		skip := 0
		for skip < len(rest) && (rest[skip] == ' ' || rest[skip] == '\t' || rest[skip] == '\r' || rest[skip] == '\n') {
			skip++
		}
		if strings.HasPrefix(rest[skip:], "```") {
			cur += skip + 3
		}
	}
	cleaned := strings.TrimSpace(squeezeBlankLines(out.String()))
	return calls, cleaned
}

// matchBraces returns the index of the `}` that closes the `{` at start,
// respecting JSON string literals. -1 if unbalanced.
func matchBraces(s string, start int) int {
	if start >= len(s) || s[start] != '{' {
		return -1
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
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
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// parseJSONToolCall recognizes the documented JSON tool-call shapes. Returns
// ok=false if body is not a tool call (caller keeps text verbatim).
func parseJSONToolCall(body string, known map[string]bool, canonical map[string]string) (parsedCall, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		// try lenient repair for trailing commas / unescaped newlines
		if repaired, ok := lenientJSONRepair(body); ok {
			if err := json.Unmarshal([]byte(repaired), &raw); err != nil {
				return parsedCall{}, false
			}
		} else {
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
		low := strings.ToLower(strings.TrimSpace(name))
		if low == "" || !known[low] {
			return parsedCall{}, false
		}
		args := map[string]any{}
		switch a := fn["arguments"].(type) {
		case map[string]any:
			args = a
		case string:
			_ = json.Unmarshal([]byte(a), &args)
		}
		return parsedCall{Name: canonical[low], Input: args}, true
	}
	// {"name":"<tool>",...}
	if name, ok := raw["name"].(string); ok {
		low := strings.ToLower(strings.TrimSpace(name))
		if low != "" && known[low] {
			return parsedCall{Name: canonical[low], Input: argsFromRaw(raw, "name")}, true
		}
	}
	// {"type":"<tool>",...}
	if t, ok := raw["type"].(string); ok {
		low := strings.ToLower(strings.TrimSpace(t))
		if low != "" && known[low] {
			return parsedCall{Name: canonical[low], Input: argsFromRaw(raw, "type")}, true
		}
	}
	return parsedCall{}, false
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
		var v map[string]any
		if err := json.Unmarshal([]byte(as), &v); err == nil {
			return v
		}
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
