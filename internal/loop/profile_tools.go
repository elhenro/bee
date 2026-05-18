package loop

import (
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// StripSchemaDescriptionsForProfile removes all "description" keys deep in a
// tool's parameter schema when the profile is "tiny". Wire-format param
// descriptions aren't part of the token budget the user sees, but they DO
// count against the provider's context window: typically ~600 tokens for
// the full toolset. Tiny models don't need them, the one-line tool summary
// in the system prompt manifest is enough.
func StripSchemaDescriptionsForProfile(spec map[string]any, profile string) map[string]any {
	if profile != "tiny" {
		return spec
	}
	out, _ := scrubDescriptionsAny(spec).(map[string]any)
	return out
}

// stripToolSpecDescriptionsForProfile applies StripSchemaDescriptionsForProfile
// to every spec's Schema. Returns the input unchanged when profile != tiny.
//
// Skips work when the profile uses tool_format=xml: the textmode wrapper
// nils req.Tools before the schema ever reaches the wire, so stripping
// descriptions saves nothing and just rewrites maps for no gain.
func stripToolSpecDescriptionsForProfile(specs []llm.ToolSpec, cfg config.Config) []llm.ToolSpec {
	if cfg.Profile != "tiny" {
		return specs
	}
	if config.ActiveProfile(cfg).ToolFormat == "xml" {
		return specs
	}
	out := make([]llm.ToolSpec, len(specs))
	for i, s := range specs {
		s.Schema = StripSchemaDescriptionsForProfile(s.Schema, cfg.Profile)
		out[i] = s
	}
	return out
}

func scrubDescriptionsAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if k == "description" {
				continue
			}
			out[k] = scrubDescriptionsAny(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = scrubDescriptionsAny(x)
		}
		return out
	}
	return v
}
