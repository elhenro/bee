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
func (p *TextModeProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	known := make(map[string]bool, len(req.Tools))
	canonical := make(map[string]string, len(req.Tools))
	for _, t := range req.Tools {
		known[strings.ToLower(t.Name)] = true
		canonical[strings.ToLower(t.Name)] = t.Name
	}

	inj := buildToolInstruction(req.Tools, p.opts.ExtraHint)
	req.System = mergeSystem(req.System, inj)
	req.Tools = nil

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
func (p *TextModeProvider) relay(in <-chan Event, out chan<- Event, known map[string]bool, canonical map[string]string) {
	defer close(out)
	var buf strings.Builder
	var done Event
	gotDone := false
	for ev := range in {
		switch ev.Type {
		case EventTextDelta:
			buf.WriteString(ev.Delta)
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
	calls, cleaned := extractToolCalls(text, known, canonical)
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
// preserved from req.Tools so callers can prioritize.
func buildToolInstruction(tools []ToolSpec, extra string) string {
	var b strings.Builder
	b.WriteString("## Tools (text format)\n")
	b.WriteString("Call a tool by emitting EXACTLY one XML block per call:\n")
	b.WriteString("<tool_name>{\"arg\":\"value\"}</tool_name>\n\n")
	b.WriteString("Available tools:\n")
	for _, t := range tools {
		desc := t.PromptSnippet
		if desc == "" {
			desc = firstSentence(t.Description)
		}
		b.WriteString("- ")
		b.WriteString(t.Name)
		b.WriteString(": ")
		b.WriteString(desc)
		b.WriteString("\n")
	}
	b.WriteString("\nEmit ONLY the XML, nothing else when calling a tool. No prose before or after the tag.\n")
	if extra != "" {
		b.WriteString("\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}
	return b.String()
}

// firstSentence picks the first period/newline-terminated chunk so the
// advert stays narrow on long descriptions.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ".\n"); i > 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
