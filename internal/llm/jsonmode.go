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
	if tool == "" && say == "" {
		return "", nil, "", false
	}
	return tool, args, say, true
}

// buildUnionSchema renders the response_format schema: one branch per tool
// plus the say branch. Single-value "enum" pins the tool name (more widely
// supported by grammar compilers than "const"). Tool order is preserved from
// req.Tools so the request body stays byte-stable across turns for a fixed
// toolset — KV-cache prefix hits depend on it.
func buildUnionSchema(tools []ToolSpec) map[string]any {
	branches := make([]any, 0, len(tools)+1)
	branches = append(branches, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"say": map[string]any{"type": "string"},
		},
		"required":             []string{"say"},
		"additionalProperties": false,
	})
	for _, t := range tools {
		argSchema := any(t.Schema)
		if len(t.Schema) == 0 {
			argSchema = map[string]any{"type": "object"}
		}
		// optional "say" on tool branches: a say-only turn ends the loop, so
		// without it the model must choose between acknowledging and acting —
		// small models then narrate progress instead of making it. say+tool
		// keeps the think-aloud-then-act shape native tool_calls allow.
		branches = append(branches, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"say":  map[string]any{"type": "string"},
				"tool": map[string]any{"enum": []string{t.Name}},
				"args": argSchema,
			},
			"required":             []string{"tool", "args"},
			"additionalProperties": false,
		})
	}
	return map[string]any{"anyOf": branches}
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
	b.WriteString("- {\"say\":\"<text>\"} alone ENDS your turn. Use only for the final answer or a question to the user.\n")
	b.WriteString("Work happens ONLY through tool calls. Saying you changed something does not change it.\n\n")
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
	b.WriteString("\nMulti-step task: call a tool every turn until done, then finish with {\"say\":...}. No prose outside the JSON object.\n")
	return b.String()
}
