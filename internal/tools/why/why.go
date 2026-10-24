// Package why adds an optional one-line `why` argument to the mutating tools.
// The reason is written at the moment of the edit, when the model knows it
// best, and rides in the tool-use input so the transcript, the session log and
// the TUI tool card all carry it. The key is stripped before the wrapped tool
// runs, and a call without it still succeeds: a missing reason never blocks work.
package why

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

// Key is the argument name shared with the TUI and the review pass.
const Key = "why"

const propDesc = "one line: why this change, not what it does"

// Mutators are the tools that take a why.
var Mutators = map[string]bool{
	"apply_patch":   true,
	"write":         true,
	"edit":          true,
	"hashline_edit": true,
}

// Of returns the trimmed why in a tool-use input, or "".
func Of(input map[string]any) string {
	s, _ := input[Key].(string)
	return strings.TrimSpace(s)
}

type wrapped struct{ tools.Tool }

// WrapAll decorates every mutator in all and leaves the rest alone.
func WrapAll(all []tools.Tool) []tools.Tool {
	for i, t := range all {
		if Mutators[t.Spec().Name] {
			all[i] = wrapped{t}
		}
	}
	return all
}

func (w wrapped) Spec() llm.ToolSpec {
	spec := w.Tool.Spec()
	props, _ := spec.Schema["properties"].(map[string]any)
	if props == nil {
		return spec
	}
	// copy so the inner tool's schema map stays untouched across calls.
	schema := make(map[string]any, len(spec.Schema))
	for k, v := range spec.Schema {
		schema[k] = v
	}
	np := make(map[string]any, len(props)+1)
	for k, v := range props {
		np[k] = v
	}
	np[Key] = map[string]any{"type": "string", "description": propDesc}
	schema["properties"] = np
	spec.Schema = schema
	return spec
}

func (w wrapped) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	if _, ok := in[Key]; ok {
		clean := make(map[string]any, len(in))
		for k, v := range in {
			if k != Key {
				clean[k] = v
			}
		}
		in = clean
	}
	return w.Tool.Run(ctx, in)
}
