package loop

import (
	"maps"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

const (
	callLabelDesc = "3-8 word label of what this call does, shown to the user, e.g. \"running the snake tests\". Lowercase, present tense."
	whyDesc       = "one line: why this change, not what it does"
)

// addDisplayArgs offers the model the optional display-only args: a
// `description` label on tools whose raw args read poorly as a card title and
// a `why` on the file mutators. Skipped on tiny: small models spend tokens on
// them and the schema budget is tight. Registry specs are shared, so a schema
// is copied, never edited in place.
func addDisplayArgs(specs []llm.ToolSpec, cfg config.Config) []llm.ToolSpec {
	if cfg.Profile == "tiny" {
		return specs
	}
	out := make([]llm.ToolSpec, len(specs))
	for i, s := range specs {
		if types.LabelsCalls(s.Name) {
			s = withArg(s, types.CallLabelKey, callLabelDesc)
		}
		if types.TakesWhy(s.Name) {
			s = withArg(s, types.WhyKey, whyDesc)
		}
		out[i] = s
	}
	return out
}

// withArg returns s with an optional string arg added, unless the tool
// already has an arg of that name.
func withArg(s llm.ToolSpec, key, desc string) llm.ToolSpec {
	props, _ := s.Schema["properties"].(map[string]any)
	if props == nil {
		return s
	}
	if _, taken := props[key]; taken {
		return s
	}
	props = maps.Clone(props)
	props[key] = map[string]any{"type": "string", "description": desc}
	s.Schema = maps.Clone(s.Schema)
	s.Schema["properties"] = props
	return s
}
