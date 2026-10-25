package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/llm"
)

// formatSchemaError renders the diagnostic the model sees. Layout:
//
//	tool args invalid for "<name>": <problem1>; <problem2>.
//	accepted args: command:string (required), timeout_seconds:integer, cwd:string
//	example: <bash>{"command":"ls -la"}</bash>
//
// The example is built from required keys with placeholder values picked per
// schema type. 3B-active local MoEs lock onto literal shapes — the example is
// the load-bearing part.
func formatSchemaError(spec llm.ToolSpec, props map[string]any, required []string, problems []string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("tool args invalid for %q: %s.\n", spec.Name, strings.Join(problems, "; ")))
	b.WriteString("accepted args: ")
	b.WriteString(renderArgList(props, required))
	b.WriteString("\nexample: ")
	b.WriteString(renderExampleEnvelope(spec.Name, props, required))
	return b.String()
}

// renderArgList produces a compact "name:type (required), name:type"
// listing for the diagnostic. Required keys sort first, then optional.
func renderArgList(props map[string]any, required []string) string {
	reqSet := map[string]bool{}
	for _, r := range required {
		reqSet[r] = true
	}
	type entry struct {
		name, typ string
		req       bool
	}
	all := make([]entry, 0, len(props))
	for name, raw := range props {
		typ := "any"
		if m, ok := raw.(map[string]any); ok {
			if t, ok := m["type"].(string); ok && t != "" {
				typ = t
			}
		}
		all = append(all, entry{name, typ, reqSet[name]})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].req != all[j].req {
			return all[i].req // required first
		}
		return all[i].name < all[j].name
	})
	parts := make([]string, 0, len(all))
	for _, e := range all {
		s := e.name + ":" + e.typ
		if e.req {
			s += " (required)"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ")
}

// renderExampleEnvelope synthesizes the canonical XML envelope with sample
// values for the required args, in required order. Single line — small models
// copy the literal shape on next turn. Uses json.Marshal to escape values
// correctly.
func renderExampleEnvelope(name string, props map[string]any, required []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<%s>{", name)
	for i, key := range required {
		var val any = "VALUE"
		if propRaw, ok := props[key]; ok {
			propMap, _ := propRaw.(map[string]any)
			val = exampleValueForType(key, propMap)
		}
		k, _ := json.Marshal(key)
		v, _ := json.Marshal(val)
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s:%s", k, v)
	}
	fmt.Fprintf(&b, "}</%s>", name)
	return b.String()
}

// exampleValueForType picks a plausible placeholder for a schema property,
// keyed off type and (when string) the property name. Naming heuristics keep
// the example readable: a `path` field gets a path-ish placeholder, a `query`
// gets a search-ish placeholder.
func exampleValueForType(key string, propMap map[string]any) any {
	typ, _ := propMap["type"].(string)
	switch typ {
	case "integer", "number":
		return 0
	case "boolean":
		return false
	case "array":
		return []any{"item"}
	case "object":
		return map[string]any{"key": "value"}
	}
	// string heuristics
	lk := strings.ToLower(key)
	switch {
	case strings.Contains(lk, "path") || strings.Contains(lk, "file"):
		return "./path/to/file"
	case strings.Contains(lk, "command") || strings.Contains(lk, "cmd"):
		return "ls -la"
	case strings.Contains(lk, "query") || strings.Contains(lk, "search"):
		return "search terms"
	case strings.Contains(lk, "url"):
		return "https://example.com"
	case strings.Contains(lk, "pattern"):
		return "regex"
	case strings.Contains(lk, "name"):
		return "name"
	}
	return "value"
}
