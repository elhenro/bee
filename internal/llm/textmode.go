package llm

import (
	"context"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
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
	kinds := paramKinds(req.Tools)
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
		names := slices.Sorted(maps.Keys(known))
		for _, name := range names[:min(4, len(names))] {
			req.Stop = append(req.Stop, "</"+name+">")
		}
	}

	innerCh, err := p.inner.Stream(ctx, req)
	if err != nil {
		return nil, err
	}

	out := make(chan Event, 16)
	go p.relay(ctx, innerCh, out, known, canonical, kinds)
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
func (p *TextModeProvider) relay(ctx context.Context, in <-chan Event, out chan<- Event, known map[string]bool, canonical map[string]string, kinds map[string]map[string]string) {
	defer close(out)
	var buf strings.Builder
	// pending is this feed's answer text so far; buf holds the rest
	think := thinkSplitter{inCall: func(pending string) bool { return openCallPending(buf.String()+pending, known) }}
	closeTags := closeTagsOf(known)
	scanned := 0 // buf bytes already checked for a closing tag
	emitThought := func(thought string) {
		if thought != "" {
			emit(ctx, out, Event{Type: EventThinkingDelta, Delta: thought})
		}
	}
	// dedup: qwen3 / hermes models occasionally re-emit the same call twice
	// in one turn (training artifact when the chat template echoes the
	// envelope into both the assistant message AND a trailing tool_calls
	// field). Fingerprint = name + canonicalized args JSON. Dispatch once
	// per stream; later identical calls dropped.
	seen := map[string]bool{}
	dispatch := func(c parsedCall) {
		c.Input = coerceArgs(c.Input, kinds[strings.ToLower(c.Name)])
		fp := callFingerprint(c)
		if seen[fp] {
			return
		}
		seen[fp] = true
		emit(ctx, out, newToolUse("", c.Name, c.Input))
	}
	done, ok := relayInner(ctx, in, out, func(ev Event) {
		// inline <think> blocks are reasoning: forward them as thinking
		// so a call quoted inside a thought is never dispatched.
		answer, thought := think.feed(ev.Delta)
		emitThought(thought)
		buf.WriteString(answer)
		// withheld content still counts as generation progress.
		emit(ctx, out, Event{Type: EventProgress, N: len(ev.Delta)})
		// only attempt early dispatch when a closing tag for ANY known
		// tool just arrived: a cheap scan of the new bytes, avoids invoking
		// the full regex extractor on every token. a close tag the extractor
		// left unconsumed is not rescanned until another one shows up.
		if hasCloseTagSince(buf.String(), scanned, closeTags) {
			early, remaining := extractIncremental(buf.String(), known, canonical)
			for _, c := range early {
				dispatch(c)
			}
			if len(early) > 0 {
				buf.Reset()
				buf.WriteString(remaining)
			}
		}
		scanned = buf.Len()
	}, func(ev Event) {
		// inner provider also emits native tool_use (e.g. when the model
		// surprises us). pass through verbatim so we don't drop signal.
		// dedup against parsed calls too — if the model duplicates via
		// both channels, only the first wins.
		if ev.ToolUse != nil {
			fp := callFingerprint(parsedCall{Name: ev.ToolUse.Name, Input: ev.ToolUse.Input})
			if seen[fp] {
				return
			}
			seen[fp] = true
		}
		emit(ctx, out, ev)
	})
	if !ok {
		return
	}
	answer, thought := think.flush()
	emitThought(thought)
	buf.WriteString(answer)
	text := buf.String()
	// snapshot pre-normalization for the nudge detector: normalization
	// strips envelope markers (e.g. `<tool_call>`) on its way to canonical
	// form, so by the time extraction fails we've lost the signal that the
	// model TRIED to emit a call. Keep the raw text to feed
	// detectMalformedEnvelope below.
	rawText := text
	// rewrite hermes / qwen3 wrappers (<tool_call>, <function=NAME>) into the
	// bee canonical `<name>{...}</name>` or bare-json shapes the existing
	// extractors handle.
	text = normalizeHermesEnvelopes(text, known)
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
		dispatch(c)
	}
	// nudge path: if zero calls dispatched all turn AND the cleaned text
	// shows malformed tool-call envelope markers, synthesize a _parse_error
	// tool use so the loop surfaces a diagnostic to the model instead of
	// silently ending the turn (which stalls progress). Without this, a
	// model that emits `function=bash>` (no leading `<`, observed from
	// qwen3-A3B with markup-stripped output) or any partial hermes envelope
	// just falls through as prose and the agent appears to hang.
	if len(seen) == 0 {
		if name, snippet, ok := detectMalformedEnvelope(rawText); ok {
			emit(ctx, out, newToolUse("", name, map[string]any{
				"_parse_error": "tool-call envelope detected but could not be parsed. emit ONE canonical block: `<NAME>{\"arg\":\"val\"}</NAME>` — replace NAME with the actual tool name. no markdown fences, no stray angle brackets.",
				"_raw_args":    snippet,
			}))
			// suppress the malformed prose so it doesn't leak into the
			// user-facing transcript on top of the nudge.
			cleaned = ""
		}
	}
	if cleaned != "" {
		emit(ctx, out, Event{Type: EventTextDelta, Delta: cleaned})
	}
	emit(ctx, out, done)
}

// envelope marker regexes for detectMalformedEnvelope. Anchored on strong
// signals (hermes/DSML/tool_call) rather than any `<...>` so legit prose
// containing angle brackets doesn't trigger a spurious nudge.
var (
	// `<function=NAME>` AND the bare `function=NAME>` variant (rendering or
	// markup-strip dropped the `<`). Used together so either form triggers.
	malformedFunctionRe  = regexp.MustCompile(`(?im)(^|[\s>])function=([a-z_][a-z0-9_\-]*)>`)
	malformedParameterRe = regexp.MustCompile(`(?i)<parameter=[a-z_]`)
	malformedToolCallRe  = regexp.MustCompile(`(?i)</?tool_call>`)
)

// detectMalformedEnvelope scans s for strong tool-call envelope signals and,
// when found, returns a best-guess tool name and a short snippet of the
// offending text. Returns ok=false when no envelope marker is present.
//
// Name resolution order: `<function=NAME>` (preferred — most specific) →
// `<tool_call>` envelope (name unknown, returns `"unknown"`) → bare
// `function=NAME>` (stripped angle bracket). Snippet caps at 240 bytes for
// the model nudge.
func detectMalformedEnvelope(s string) (name, snippet string, ok bool) {
	if s == "" {
		return "", "", false
	}
	if m := malformedFunctionRe.FindStringSubmatch(s); m != nil {
		return strings.ToLower(m[2]), truncate(s, 240), true
	}
	if malformedParameterRe.MatchString(s) || malformedToolCallRe.MatchString(s) {
		return "unknown", truncate(s, 240), true
	}
	return "", "", false
}

// callFingerprint canonicalizes a parsed tool call for dedup. Uses
// json.Marshal on args (Go sorts map keys when marshaling), so semantically
// identical args with reordered keys collide as expected.
func callFingerprint(c parsedCall) string {
	buf, _ := json.Marshal(c.Input)
	return c.Name + "|" + string(buf)
}
