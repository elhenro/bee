package llm

import (
	"regexp"
	"strings"
)

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
var dsmlParamRe = regexp.MustCompile(`(?is)<\x{FF5C}+\s*DSML\s*\x{FF5C}+\s*parameter\s+name\s*=\s*["']([a-z_][a-z0-9_\-]*)["'](?:\s+string\s*=\s*["'](true|false)["'])?\s*>\s*(.*?)\s*</\s*\x{FF5C}+\s*DSML\s*\x{FF5C}+\s*parameter\s*>`)

// normalizeDSMLEnvelopes turns DSML invoke blocks into the canonical
// `<NAME>{json}</NAME>` shape and drops the surrounding
// `<｜｜DSML｜｜tool_calls>` wrapper. Each `<｜｜DSML｜｜parameter name="K" …>V</…>`
// inside an invoke becomes one JSON key. Re-running this on already-normalized
// text is a no-op since the invoke regex won't match the rewritten envelope.
func normalizeDSMLEnvelopes(s string) string {
	if !strings.Contains(s, "DSML") {
		return s
	}
	s = rewriteInvokes(s, dsmlInvokeOpenRe, dsmlBounds, dsmlParams)
	// drop tool_calls wrappers wherever they remain.
	return dsmlToolCallsWrapperRe.ReplaceAllString(s, "")
}

// dsmlBounds ends an invoke body at its </…DSML…invoke> close OR the next
// <…DSML…invoke … > open (model emitted two calls without closing the first —
// rare but observed), the latter left unconsumed. Missing both consumes the
// rest of the buffer, which tolerates a stop sequence cutting the stream past
// the first param close.
func dsmlBounds(tail, _ string) (int, int) {
	return bodyBounds(tail,
		dsmlInvokeCloseRe.FindStringIndex,
		func(t string) []int {
			loc := dsmlInvokeOpenRe.FindStringIndex(t)
			if loc != nil {
				loc[1] = loc[0]
			}
			return loc
		},
	)
}

// dsmlParams collects DSML parameter blocks. The string attr is the template's
// own type hint: only an explicit string="false" is decoded as a scalar;
// anything else stays text (schema coercion in relay handles typed params).
func dsmlParams(body string) map[string]any {
	params := dsmlParamRe.FindAllStringSubmatch(body, -1)
	args := make(map[string]any, len(params))
	for _, p := range params {
		v := strings.TrimSpace(p[3])
		if strings.EqualFold(p[2], "false") {
			args[p[1]] = decodeHermesScalar(v)
		} else {
			args[p[1]] = stripCodeFence(v)
		}
	}
	return args
}
