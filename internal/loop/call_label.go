package loop

import (
	"maps"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

const callLabelDesc = "3-8 word label of what this call does, shown to the user, e.g. \"running the snake tests\". Lowercase, present tense."

// addCallLabels offers the model an optional `description` arg on tools whose
// raw args read poorly as a card title. Skipped on tiny: small models spend
// tokens on it and the schema budget is tight. Registry specs are shared, so
// the schema is copied, never edited in place.
func addCallLabels(specs []llm.ToolSpec, cfg config.Config) []llm.ToolSpec {
	if cfg.Profile == "tiny" {
		return specs
	}
	out := make([]llm.ToolSpec, len(specs))
	for i, s := range specs {
		out[i] = s
		props, _ := s.Schema["properties"].(map[string]any)
		if !types.LabelsCalls(s.Name) || props == nil {
			continue
		}
		if _, taken := props[types.CallLabelKey]; taken {
			continue
		}
		withProps := maps.Clone(props)
		withProps[types.CallLabelKey] = map[string]any{"type": "string", "description": callLabelDesc}
		schema := maps.Clone(s.Schema)
		schema["properties"] = withProps
		out[i].Schema = schema
	}
	return out
}
