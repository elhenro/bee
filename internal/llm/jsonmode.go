package llm

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/elhenro/bee/internal/types"
	"github.com/google/uuid"
)

// JSONModeProvider wraps an inner Provider to route tool calls through
// grammar-constrained JSON output instead of the native tool_calls channel.
// Grammar-capable local servers (ollama ≥0.5, llama.cpp, MLX servers with
// xgrammar) enforce the schema at sampling time, so a malformed tool call
// is impossible by construction — the whole format-nudge recovery path
// becomes dead weight. The wrapper:
//   - strips Request.Tools and injects a short instruction block describing
//     each tool plus the one-object-per-turn JSON envelope,
//   - sets Request.ResponseSchema to a union schema: {"say":...} to talk,
//     {"tool":...,"args":{...}} to act,
//   - buffers the (JSON) completion and emits EventToolUse or the say text.
//
// Same wrapper shape as TextModeProvider so every adapter gains the mode
// for free; opt in per profile via tool_format = "json".
type JSONModeProvider struct {
	inner Provider
}

// NewJSONMode wraps inner with the constrained-JSON tool-call mode.
func NewJSONMode(inner Provider) *JSONModeProvider {
	return &JSONModeProvider{inner: inner}
}

// Name forwards to inner with a "+jsonmode" suffix so logs/UIs can tell.
func (p *JSONModeProvider) Name() string { return p.inner.Name() + "+jsonmode" }

// Stream injects the instruction block, swaps Tools for ResponseSchema, then
// runs the inner stream. Side-LLM calls (classifier, recap, compact) pass
// req.Tools == nil and bypass the wrapper entirely — they need free text.
func (p *JSONModeProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	if len(req.Tools) == 0 {
		return p.inner.Stream(ctx, req)
	}
	req.System = mergeSystem(req.System, buildJSONInstruction(req.Tools))
	req.ResponseSchema = buildUnionSchema(req.Tools)
	req.Tools = nil

	innerCh, err := p.inner.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan Event, 16)
	go p.relay(innerCh, out)
	return out, nil
}

// relay forwards thinking deltas, buffers text deltas (the raw JSON envelope
// is noise to the user), and on EventDone parses the buffer into a ToolUse or
// say text. Parse failure (server without grammar support slipped format)
// falls back to emitting the raw text so the loop's existing recovery nudges
// still see what the model tried.
func (p *JSONModeProvider) relay(in <-chan Event, out chan<- Event) {
	defer close(out)
	var buf strings.Builder
	var done Event
	gotDone := false
	for ev := range in {
		switch ev.Type {
		case EventTextDelta:
			buf.WriteString(ev.Delta)
			// withheld content still counts as generation progress.
			out <- Event{Type: EventProgress, N: len(ev.Delta)}
		case EventThinkingDelta:
			out <- ev
		case EventToolUse:
			// inner provider emitted a native call despite Tools being nil
			// (model surprise). pass through so signal isn't dropped.
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
	if name, args, say, ok := parseEnvelope(buf.String()); ok {
		// say first so the status note precedes the tool card in transcripts.
		if say != "" {
			out <- Event{Type: EventTextDelta, Delta: say}
		}
		if name != "" {
			out <- Event{Type: EventToolUse, ToolUse: &types.ToolUse{
				ID:    "call_" + uuid.NewString(),
				Name:  name,
				Input: args,
			}}
		}
	} else if text := strings.TrimSpace(buf.String()); text != "" {
		out <- Event{Type: EventTextDelta, Delta: text}
	}
	if gotDone {
		out <- done
	} else {
		out <- Event{Type: EventDone, StopReason: "stop"}
	}
}

// parseEnvelope decodes the first JSON object in s and maps it to the union
// shape. Liberal on input: trailing text after the object is ignored (grammar
// servers may pad whitespace). A combined {"say":...,"tool":...} object is
// the ack-then-act shape and yields both.
func parseEnvelope(s string) (tool string, args map[string]any, say string, ok bool) {
	s = strings.TrimSpace(s)
	// some templates wrap output in markdown fences despite instructions.
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '{'); i > 0 {
		s = s[i:]
	}
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(s))
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return "", nil, "", false
	}
	if t, _ := obj["tool"].(string); t != "" {
		tool = t
		if a, isMap := obj["args"].(map[string]any); isMap {
			args = a
		} else {
			args = map[string]any{}
		}
	}
	say, _ = obj["say"].(string)
	// "done" is the terminal branch; "say" stays accepted for the combined
	// tool+note shape and for degraded servers emitting the legacy form.
	if d, _ := obj["done"].(string); d != "" && say == "" {
		say = d
	}
	if tool == "" && say == "" {
		return "", nil, "", false
	}
	return tool, args, say, true
}

// buildUnionSchema renders the response_format schema: a "done" terminal
// branch plus one tool branch with the tool name pinned by enum and args left
// as a plain object. Tool order is preserved from req.Tools so the request
// body stays byte-stable across turns — KV-cache prefix hits depend on it.
//
// Two shapes here are empirical, isolated by live replay-bisecting against a
// sparse-MoE thinking build (full request held constant, schema swapped):
//   - terminal is "done", not bare "say": models narrate into an open say
//     slot ("Creating hello.txt...") and end the run having done nothing,
//     repeating the announcement even after a nudge.
//   - args are deliberately UNTYPED: embedding each tool's strict arg schema
//     (nested required, minLength) flipped the same model from a correct
//     tool call to a done-claim with identical prompts. The grammar pins
//     JSON-ness and the tool name; arg keys are validated post-hoc by the
//     tool layer, which also hints near-miss names.
func buildUnionSchema(tools []ToolSpec) map[string]any {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return map[string]any{"anyOf": []any{
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"done": map[string]any{"type": "string"},
			},
			"required":             []string{"done"},
			"additionalProperties": false,
		},
		// optional "say" keeps the think-aloud-then-act shape native
		// tool_calls allow; a say note rides along with the call.
		jmToolBranch{
			Type: "object",
			Properties: jmToolProps{
				Say:  map[string]any{"type": "string"},
				Tool: map[string]any{"enum": names},
				Args: map[string]any{"type": "object"},
			},
			Required:             []string{"tool", "args"},
			AdditionalProperties: false,
		},
	}}
}

// jmToolBranch / jmToolProps pin the wire-side key order of the tool branch.
// xgrammar compiles object properties in DECLARED order and only permits that
// order at sampling time; Go maps marshal alphabetically, which put "args"
// before "tool" and forced every tool call to open with {"args": — a live
// sparse-MoE build fled to the done branch instead of fighting that prefix.
// Structs marshal in field order: say, tool, args.
type jmToolBranch struct {
	Type                 string      `json:"type"`
	Properties           jmToolProps `json:"properties"`
	Required             []string    `json:"required"`
	AdditionalProperties bool        `json:"additionalProperties"`
}

type jmToolProps struct {
	Say  map[string]any `json:"say"`
	Tool map[string]any `json:"tool"`
	Args map[string]any `json:"args"`
}

// buildJSONInstruction renders the prompt-side half of the mode: the grammar
// guarantees shape, this block supplies semantics (what each tool does, which
// envelope means what). Mirrors textmode's advert but much shorter — no
// format-discipline warnings needed when the sampler enforces the format.
func buildJSONInstruction(tools []ToolSpec) string {
	var b strings.Builder
	b.WriteString("## Tools (json format)\n")
	b.WriteString("Respond with EXACTLY one JSON object per turn:\n")
	b.WriteString("- {\"tool\":\"<name>\",\"args\":{...}} runs a tool. args use the EXACT parameter names below. Optional \"say\" alongside for a short status note.\n")
	b.WriteString("- {\"done\":\"<final answer>\"} ENDS the task. Emit only when the work is fully done, or to ask the user a blocking question.\n")
	b.WriteString("Never announce what you are about to do. Either do it now ({\"tool\":...}) or finish ({\"done\":...}).\n")
	b.WriteString("Work happens ONLY through tool calls. Claiming a change without a tool call does not make it.\n\n")
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
	b.WriteString("\nMulti-step task: call a tool every turn until done, then finish with {\"done\":...}. No prose outside the JSON object.\n")
	return b.String()
}
