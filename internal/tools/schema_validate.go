// Schema-driven input validation for tool args. Runs in the loop BEFORE
// each tool's own Run, so a model that emits valid JSON but the wrong key
// names sees a concrete corrected example instead of a generic "missing X"
// from inside the tool.
//
// Why centralized: every tool hand-rolls required-key checks today and emits
// a plain-text "missing or empty 'foo' field" message. Small local models
// (qwen3-a3b, gemma3) need a literal example envelope to lock onto. This
// validator returns one, derived from the tool's own JSON Schema.
package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/llm"
)

// ValidateInput checks that input contains all required keys from spec.Schema
// and that present values match the declared JSON Schema type. Returns nil on
// success. On failure returns a diagnostic string suitable for sending back
// to the model: lists what is wrong, lists the accepted keys with types, and
// finally a one-shot XML envelope example so 3B-active MoEs can copy it.
//
// Type checks are intentionally lenient: JSON numbers decode as float64, so
// "integer" accepts float64 with an integral value. Strings, bools, arrays
// and objects map straight through. Unknown schema types skip checking.
func ValidateInput(spec llm.ToolSpec, input map[string]any) error {
	if len(spec.Schema) == 0 {
		return nil
	}
	props, _ := spec.Schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil
	}
	required := schemaRequired(spec.Schema)
	var problems []string

	// missing-required check first — most common failure on tiny models.
	for _, key := range required {
		v, ok := input[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("missing required %q", key))
			continue
		}
		if isEmptyValue(v) {
			problems = append(problems, fmt.Sprintf("empty %q", key))
		}
	}

	// wrong-type check on present keys. Skip required keys already flagged
	// missing above so the diagnostic doesn't repeat.
	missingSet := map[string]bool{}
	for _, p := range problems {
		missingSet[p] = true
	}
	for key, raw := range input {
		if strings.HasPrefix(key, "_") {
			continue // _parse_error, _raw_args, etc.
		}
		propRaw, ok := props[key]
		if !ok {
			continue // unknown key — tolerate, don't reject
		}
		propMap, _ := propRaw.(map[string]any)
		want, _ := propMap["type"].(string)
		if want == "" {
			continue
		}
		if got, ok := jsonTypeMismatch(raw, want); !ok {
			problems = append(problems, fmt.Sprintf("wrong type for %q: expected %s, got %s", key, want, got))
			continue
		}
		// minLength on strings rejects blank optional args too, not just
		// required ones. skip keys already flagged empty above.
		if s, ok := raw.(string); ok && !missingSet[fmt.Sprintf("empty %q", key)] {
			if min, ok := schemaMinLength(propMap); ok && len(strings.TrimSpace(s)) < min {
				problems = append(problems, fmt.Sprintf("%q too short: needs at least %d non-blank char(s)", key, min))
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s", formatSchemaError(spec, props, required, problems))
}

// schemaRequired pulls the required slice from a schema regardless of whether
// it deserialized as []string or []any (json.Unmarshal yields []any for raw
// JSON, but hand-coded Go schemas often use []string).
func schemaRequired(schema map[string]any) []string {
	switch rs := schema["required"].(type) {
	case []string:
		out := make([]string, len(rs))
		copy(out, rs)
		return out
	case []any:
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			if s, ok := r.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// schemaMinLength reads minLength off a property map. json.Unmarshal yields
// float64 for numbers, hand-coded Go schemas use int — accept both.
func schemaMinLength(propMap map[string]any) (int, bool) {
	switch n := propMap["minLength"].(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// isEmptyValue trips on the most common "model sent the key but value is
// blank" pattern: empty strings, empty slices, empty maps. Numbers and bools
// are never "empty" — zero is a legitimate value.
func isEmptyValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	}
	return false
}

// jsonTypeMismatch returns ("", true) when raw matches the declared JSON
// Schema type, or (gotName, false) otherwise. gotName is the JSON-ish label
// for the actual value, used in the diagnostic.
func jsonTypeMismatch(raw any, want string) (string, bool) {
	switch want {
	case "string":
		if _, ok := raw.(string); ok {
			return "", true
		}
	case "integer":
		switch n := raw.(type) {
		case int, int32, int64:
			return "", true
		case float64:
			if n == float64(int64(n)) {
				return "", true
			}
			return "number", false
		}
	case "number":
		switch raw.(type) {
		case int, int32, int64, float64:
			return "", true
		}
	case "boolean":
		if _, ok := raw.(bool); ok {
			return "", true
		}
	case "array":
		if _, ok := raw.([]any); ok {
			return "", true
		}
	case "object":
		if _, ok := raw.(map[string]any); ok {
			return "", true
		}
	default:
		return "", true // unknown schema type — don't reject
	}
	return jsonTypeName(raw), false
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, int, int32, int64:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", v)
}

// formatSchemaError renders the diagnostic the model sees. Layout:
//
//   tool args invalid for "<name>": <problem1>; <problem2>.
//   accepted args: command:string (required), timeout_seconds:integer, cwd:string
//   example: <bash>{"command":"ls -la"}</bash>
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
// values for the required args. Single line — small models copy the literal
// shape on next turn. Uses json.Marshal to escape values correctly.
func renderExampleEnvelope(name string, props map[string]any, required []string) string {
	example := map[string]any{}
	for _, key := range required {
		propRaw, ok := props[key]
		if !ok {
			example[key] = "VALUE"
			continue
		}
		propMap, _ := propRaw.(map[string]any)
		example[key] = exampleValueForType(key, propMap)
	}
	// keys sorted for deterministic example (KV-cache friendly).
	buf, err := json.Marshal(orderedJSON(example, required))
	if err != nil {
		buf = []byte(`{"...":"..."}`)
	}
	return fmt.Sprintf("<%s>%s</%s>", name, string(buf), name)
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

// orderedJSON wraps a map so json.Marshal emits keys in the order from
// `order`. Keys not in `order` are skipped — the example only includes the
// required keys, so optional keys naturally drop out.
type orderedJSONMap struct {
	m     map[string]any
	order []string
}

func orderedJSON(m map[string]any, order []string) orderedJSONMap {
	return orderedJSONMap{m: m, order: order}
}

func (o orderedJSONMap) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	first := true
	for _, k := range o.order {
		v, ok := o.m[k]
		if !ok {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		key, _ := json.Marshal(k)
		val, _ := json.Marshal(v)
		b.Write(key)
		b.WriteByte(':')
		b.Write(val)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}
