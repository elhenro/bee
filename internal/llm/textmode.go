package llm

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/elhenro/bee/internal/types"
)

// TextModeProvider wraps an inner Provider to bypass the native tool_calls
// channel. Many small local models (llama3.1:8b, gemma3, phi3) silently
// ignore function-calling deltas but reliably emit inline XML when shown
// one example. The wrapper:
//   - strips Request.Tools and injects a text instruction block describing
//     each tool plus the `<tool>{...}</tool>` envelope,
//   - buffers assistant text deltas,
//   - scans the buffered text after the stream ends for tool-call tags,
//     synthesizes EventToolUse for each, and emits the cleaned text.
//
// Why a wrapper instead of a separate provider: every existing adapter
// (openai_compat, chatgpt, claude, gemini) gains XML-mode for free.
type TextModeProvider struct {
	inner Provider
	opts  TextModeOptions
}

// TextModeOptions tunes the wrapper. All fields are optional.
type TextModeOptions struct {
	// ExtraHint is appended verbatim after the auto-generated tool block.
	// Useful for caveman-style brevity nudges.
	ExtraHint string
}

// NewTextMode wraps inner with the text/XML tool-call fallback.
func NewTextMode(inner Provider, opts TextModeOptions) *TextModeProvider {
	return &TextModeProvider{inner: inner, opts: opts}
}

// Name forwards to inner with a "+textmode" suffix so logs/UIs can tell.
func (p *TextModeProvider) Name() string { return p.inner.Name() + "+textmode" }

// Stream injects the text-tool instruction block, nils Tools, then runs the
// inner stream. Tool-call extraction happens at EventDone.
//
// Side-LLM calls (mode classifier, recap, compact) pass req.Tools == nil. In
// that case skip injection entirely — pumping a `## Tools (text format)`
// block with an empty tool list into a classifier/recap prompt pollutes the
// instruction and pushes small models toward emitting spurious XML.
func (p *TextModeProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if len(req.Tools) == 0 {
		return p.inner.Stream(ctx, req)
	}
	known := make(map[string]bool, len(req.Tools))
	canonical := make(map[string]string, len(req.Tools))
	for _, t := range req.Tools {
		known[strings.ToLower(t.Name)] = true
		canonical[strings.ToLower(t.Name)] = t.Name
	}

	inj := buildToolInstruction(req.Tools, p.opts.ExtraHint)
	req.System = mergeSystem(req.System, inj)
	req.Tools = nil

	// stop sequences: halt decode at first tool-call close tag. Saves
	// 50–300 tokens per turn on 3B-active local MoEs that otherwise ramble
	// past the close tag. Only auto-injected when caller hasn't set Stop.
	// Cap at 4 — most provider APIs limit `stop` to 4 entries.
	//
	// Order is sorted so the same input toolset produces the same stop list
	// every turn — KV-cache prefix hits depend on the request body being
	// byte-stable for a fixed prompt.
	if len(req.Stop) == 0 {
		names := make([]string, 0, len(known))
		for name := range known {
			names = append(names, name)
		}
		sortStable(names)
		for _, name := range names {
			req.Stop = append(req.Stop, "</"+name+">")
			if len(req.Stop) >= 4 {
				break
			}
		}
	}

	innerCh, err := p.inner.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	out := make(chan Event, 16)
	go p.relay(innerCh, out, known, canonical)
	return out, nil
}

// relay forwards thinking deltas immediately, buffers text deltas, and on
// EventDone scans the buffer for tool tags. Synthesized ToolUse events are
// emitted in source order before the (now cleaned) text and finally Done.
//
// Incremental fast-path: on each text delta, if the buffer now contains a
// closing tag for any known tool, run the extractors immediately and emit
// the synthesized ToolUse events early. Cuts post-stream latency on
// providers that emit one delta per token by amortizing the parse over the
// stream instead of doing it all at EventDone.
func (p *TextModeProvider) relay(in <-chan Event, out chan<- Event, known map[string]bool, canonical map[string]string) {
	defer close(out)
	var buf strings.Builder
	var done Event
	gotDone := false
	for ev := range in {
		switch ev.Type {
		case EventTextDelta:
			buf.WriteString(ev.Delta)
			// only attempt early dispatch when a closing tag for ANY known
			// tool is present — cheap substring scan, avoids invoking the
			// full regex extractor on every token.
			if hasAnyCloseTag(buf.String(), known) {
				early, remaining := extractIncremental(buf.String(), known, canonical)
				for _, c := range early {
					out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
						ID:    "call_" + uuid.NewString(),
						Name:  c.Name,
						Input: c.Input,
					}}
				}
				if len(early) > 0 {
					buf.Reset()
					buf.WriteString(remaining)
				}
			}
		case EventThinkingDelta:
			out <- ev
		case EventToolUse:
			// inner provider also emits native tool_use (e.g. when the model
			// surprises us). pass through verbatim so we don't drop signal.
			out <- ev
		case EventDone:
			done = ev
			gotDone = true
		case EventError:
			out <- ev
			return
		default:
			out <- ev
		}
	}
	text := buf.String()
	// rewrite hermes / qwen3 wrappers (<tool_call>, <function=NAME>) into the
	// bee canonical `<name>{...}</name>` or bare-json shapes the existing
	// extractors handle.
	text = normalizeHermesEnvelopes(text)
	calls, cleaned := extractToolCalls(text, known, canonical)
	// fallback: model emitted JSON tool-call envelope instead of XML.
	// scan the remaining (XML-stripped) text for bare {"type":"<tool>",...}
	// or {"name":"<tool>",...} shapes. seen with both small local models
	// and big hosted reasoners when textmode is forced.
	if jsonCalls, jsonCleaned := extractJSONToolCalls(cleaned, known, canonical); len(jsonCalls) > 0 {
		calls = append(calls, jsonCalls...)
		cleaned = jsonCleaned
	}
	for _, c := range calls {
		out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
			ID:    "call_" + uuid.NewString(),
			Name:  c.Name,
			Input: c.Input,
		}}
	}
	if cleaned != "" {
		out <- Event{Type: EventTextDelta, Delta: cleaned}
	}
	if gotDone {
		out <- done
	} else {
		out <- Event{Type: EventDone, StopReason: "stop"}
	}
}

// hasAnyCloseTag is a cheap substring scan: returns true when s contains a
// closing tag for any known tool. Cheaper than firing the regex extractor
// on every token-sized delta.
func hasAnyCloseTag(s string, known map[string]bool) bool {
	if len(s) == 0 || len(known) == 0 {
		return false
	}
	for name := range known {
		if strings.Contains(s, "</"+name+">") {
			return true
		}
	}
	return false
}

// extractIncremental runs the hermes normalizer + tool extractors on the
// current buffer and returns (early-dispatched calls, remaining buf). The
// remaining buffer is what comes AFTER the last successful close tag — text
// past that point hasn't been parsed yet and stays for the next delta cycle.
//
// Conservative: when no closed tag is found, returns (nil, buf unchanged)
// so the caller knows nothing was consumed.
func extractIncremental(s string, known map[string]bool, canonical map[string]string) ([]parsedCall, string) {
	// find the last close tag position. anything past it is partial and
	// must stay in the buffer.
	lastClose := -1
	for name := range known {
		tag := "</" + name + ">"
		if idx := strings.LastIndex(s, tag); idx >= 0 {
			end := idx + len(tag)
			if end > lastClose {
				lastClose = end
			}
		}
	}
	if lastClose < 0 {
		return nil, s
	}
	head := s[:lastClose]
	tail := s[lastClose:]
	normalized := normalizeHermesEnvelopes(head)
	calls, cleaned := extractToolCalls(normalized, known, canonical)
	if len(calls) == 0 {
		return nil, s
	}
	// preserve surrounding prose: cleaned is `head` with tool tags removed,
	// tail is the not-yet-parsed text past the last close. EventDone's final
	// extractToolCalls pass will see `cleaned + tail` — no closed tags
	// remain in cleaned (extractor already consumed them), so it falls
	// through as plain text in the final EventTextDelta.
	remaining := cleaned
	if tail != "" {
		if remaining != "" {
			remaining += " "
		}
		remaining += tail
	}
	return calls, remaining
}

// mergeSystem appends the injected block to an existing system prompt with a
// blank line between. Empty system → injection only.
func mergeSystem(sys, inj string) string {
	sys = strings.TrimRight(sys, "\n")
	if sys == "" {
		return inj
	}
	return sys + "\n\n" + inj
}

// buildToolInstruction renders the text-mode tool advertisement. Order is
// preserved from req.Tools so callers can prioritize. Parameter names + types
// from each tool's JSON Schema are surfaced so the model emits the right keys
// instead of guessing (qwen3 used to invent `{"args":{"cmd":"..."}}` here).
func buildToolInstruction(tools []ToolSpec, extra string) string {
	var b strings.Builder
	b.WriteString("## Tools (text format)\n")
	b.WriteString("Call a tool by emitting EXACTLY one XML block per call. The tag NAME is the tool's name verbatim (e.g. <bash>, <edit>, <write>) — NOT the literal string \"tool_name\":\n")
	b.WriteString("<TOOLNAME>{\"arg\":\"value\"}</TOOLNAME>\n\n")
	b.WriteString("Args must use EXACT parameter names from each tool's signature below.\n")
	b.WriteString("DO NOT invent keys like `args`/`cmd`/`input` — use the schema names verbatim.\n")
	b.WriteString("Accepted alternative shapes (parsed as fallback):\n")
	b.WriteString("- bare JSON: `{\"name\":\"tool\",\"arguments\":{...}}`\n")
	b.WriteString("- hermes wrapper: `<tool_call>{\"name\":\"tool\",\"arguments\":{...}}</tool_call>`\n\n")
	b.WriteString("Available tools:\n")
	for _, t := range tools {
		desc := t.PromptSnippet
		if desc == "" {
			desc = firstSentence(t.Description)
		}
		b.WriteString("- ")
		b.WriteString(t.Name)
		if sig := renderSchemaSig(t.Schema); sig != "" {
			b.WriteString(sig)
		}
		b.WriteString(": ")
		b.WriteString(desc)
		b.WriteString("\n")
	}
	b.WriteString("\nEmit ONLY the XML, nothing else when calling a tool. No prose before or after the tag.\n")
	// 1-shot exemplar: 3B-active MoEs (qwen3-a3b, etc.) lock onto literal
	// shapes more reliably than abstract spec. Anchor on the most common
	// tool name (bash) so the template imprints — saves a re-try on first call.
	b.WriteString("\nExample (verbatim shape — match exactly):\n")
	b.WriteString("<bash>{\"command\":\"ls -la\"}</bash>\n")
	if extra != "" {
		b.WriteString("\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}
	return b.String()
}

// renderSchemaSig turns a JSON Schema object into a compact param signature.
// e.g. `(command:string, [timeout_seconds:integer], [cwd:string])`.
// Required params first, optional in `[brackets]`. Returns "" for empty/no
// properties so tools without schema fall back to bare `- name: desc`.
func renderSchemaSig(schema map[string]any) string {
	if len(schema) == 0 {
		return ""
	}
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return ""
	}
	required := map[string]bool{}
	if rs, ok := schema["required"].([]any); ok {
		for _, r := range rs {
			if s, ok := r.(string); ok {
				required[s] = true
			}
		}
	} else if rs, ok := schema["required"].([]string); ok {
		for _, s := range rs {
			required[s] = true
		}
	}
	var req, opt []string
	for name, raw := range props {
		typ := "any"
		if m, ok := raw.(map[string]any); ok {
			if t, ok := m["type"].(string); ok && t != "" {
				typ = t
			}
		}
		entry := name + ":" + typ
		if required[name] {
			req = append(req, entry)
		} else {
			opt = append(opt, "[" + entry + "]")
		}
	}
	sortStable(req)
	sortStable(opt)
	all := append(req, opt...)
	if len(all) == 0 {
		return ""
	}
	return "(" + strings.Join(all, ", ") + ")"
}

// sortStable: minimal alphabetic sort without pulling in sort just for a tiny
// list of param names. Keeps `command` before `cwd` deterministically across
// runs so transcripts diff cleanly.
func sortStable(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j-1] > ss[j]; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}

// firstSentence picks a short lead from a tool's description so the advert
// stays narrow. Prefers the first newline; otherwise cuts at a period that
// is followed by whitespace so URLs (`https://example.com/foo`) and version
// strings (`v1.2.3`) don't get sliced mid-token. Returns the full string
// when neither marker is found.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	for i := 0; i < len(s)-1; i++ {
		if s[i] != '.' {
			continue
		}
		switch s[i+1] {
		case ' ', '\t':
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}
