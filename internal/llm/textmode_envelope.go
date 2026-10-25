package llm

import (
	"encoding/json"
	"regexp"
	"strings"
)

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

// hermesParamRe captures one `<parameter=KEY>VALUE</parameter>` block. Used
// from inside hermesFunctionRe match bodies.
var hermesParamRe = regexp.MustCompile(`(?is)<parameter=([a-z_][a-z0-9_\-]*)>\s*(.*?)\s*</parameter>`)

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
func normalizeHermesEnvelopes(s string, known map[string]bool) string {
	// pass 0: rewrite attribute-style `<NAME k="v" k="v"/>` (and explicit
	// close form `<NAME k="v">...</NAME>`) into canonical
	// `<NAME>{"k":"v",...}</NAME>`. catches the HTML/XML-flavored envelope
	// some models reach for when the JSON-in-body form doesn't stick. tool
	// name resolution is deferred to extractToolCalls.
	s = normalizeAttrEnvelopes(s, known)
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
	return rewriteInvokes(s, hermesFunctionOpenRe, hermesBounds, hermesParams)
}

// envelope renders the canonical `<NAME>{json}</NAME>` tool call. args hold
// strings and decoded JSON, so marshaling cannot fail.
func envelope(name string, args map[string]any) string {
	buf, _ := json.Marshal(args)
	return "<" + name + ">" + string(buf) + "</" + name + ">"
}

// rewriteInvokes replaces every block that openRe opens (tool name in group 1)
// with a canonical envelope. bounds splits the text after the open tag into
// body end and resume offset, params builds the args from the body.
func rewriteInvokes(s string, openRe *regexp.Regexp, bounds func(tail, name string) (bodyEnd, advance int), params func(body string) map[string]any) string {
	var b strings.Builder
	b.Grow(len(s))
	for {
		loc := openRe.FindStringSubmatchIndex(s)
		if loc == nil {
			break
		}
		name := strings.ToLower(s[loc[2]:loc[3]])
		tail := s[loc[1]:]
		bodyEnd, advance := bounds(tail, name)
		b.WriteString(s[:loc[0]])
		b.WriteString(envelope(name, params(tail[:bodyEnd])))
		s = tail[advance:]
	}
	b.WriteString(s)
	return b.String()
}

// bodyBounds returns where a block body ends and where scanning resumes: the
// earliest match of any finder, first listed wins a tie. a finder returns the
// [start,end) of its marker, nil when absent. no marker means the body takes
// the rest of the buffer.
func bodyBounds(tail string, finders ...func(string) []int) (bodyEnd, advance int) {
	bodyEnd, advance = len(tail), len(tail)
	for _, find := range finders {
		if loc := find(tail); loc != nil && loc[0] < bodyEnd {
			bodyEnd, advance = loc[0], loc[1]
		}
	}
	return bodyEnd, advance
}

// hermesBounds ends a <function=NAME> body at </function> or </NAME>. Whichever
// close appears FIRST wins so a body that contains a literal "</function>"
// followed later by a real "</NAME>" doesn't accidentally swallow further
// content.
func hermesBounds(tail, name string) (int, int) {
	return bodyBounds(tail,
		func(t string) []int { return foldLoc(t, "</function>") },
		func(t string) []int { return foldLoc(t, "</"+name+">") },
	)
}

// hermesParams collects `<parameter=K>V</parameter>` blocks. Bodies are raw
// text with no type marker, so they stay strings here; relay coerces them
// against the tool schema (coerceArgs), which keeps a JSON-looking file body a
// string.
func hermesParams(body string) map[string]any {
	params := hermesParamRe.FindAllStringSubmatch(body, -1)
	args := make(map[string]any, len(params))
	for _, p := range params {
		args[p[1]] = stripCodeFence(p[2])
	}
	return args
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
		i := skipSpace(tail, 0)
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
		b.WriteString("<" + name + ">" + tail[i:end+1] + "</" + name + ">")
		cur = matchEnd + end + 1
	}
	return b.String()
}

// unwrapToolCallEnvelopes replaces <tool_call>BODY</tool_call> with BODY, AND
// also strips a dangling `<tool_call>` opener with no close — happens when the
// textmode stop sequence (`</NAME>`) terminates the stream past the function
// body but before the model emits </tool_call>.
func unwrapToolCallEnvelopes(s string) string {
	s = hermesToolCallRe.ReplaceAllString(s, "$1")
	// dangling open with no matching close — strip just the opener.
	return strings.ReplaceAll(s, "<tool_call>", "")
}
