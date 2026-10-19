package llm

import (
	"encoding/json"
	"strings"
)

// paramKinds maps each tool (lowercase name) to its parameters' declared JSON
// schema types. Params without a type are left out.
func paramKinds(tools []ToolSpec) map[string]map[string]string {
	out := make(map[string]map[string]string, len(tools))
	for _, t := range tools {
		props, _ := t.Schema["properties"].(map[string]any)
		kinds := map[string]string{}
		for name, raw := range props {
			if m, ok := raw.(map[string]any); ok {
				if typ, _ := m["type"].(string); typ != "" {
					kinds[name] = typ
				}
			}
		}
		out[strings.ToLower(t.Name)] = kinds
	}
	return out
}

// coerceArgs decodes string args whose schema type is not string (number,
// boolean, array, object), for envelopes that carry no types of their own
// (hermes parameters). A value that does not decode to the declared kind, and
// any param typed string or untyped, is left as written.
func coerceArgs(in map[string]any, kinds map[string]string) map[string]any {
	if len(kinds) == 0 {
		return in
	}
	for k, v := range in {
		s, ok := v.(string)
		typ := kinds[k]
		if !ok || typ == "" || typ == "string" {
			continue
		}
		var out any
		if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &out); err != nil {
			continue
		}
		if kindMatches(typ, out) {
			in[k] = out
		}
	}
	return in
}

func kindMatches(typ string, v any) bool {
	switch typ {
	case "integer", "number":
		_, ok := v.(float64)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	}
	return false
}
