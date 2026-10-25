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
	var problems, missingRequired []string
	emptyKeys := map[string]bool{}

	// missing-required check first — most common failure on tiny models.
	for _, key := range required {
		v, ok := input[key]
		if !ok {
			problems = append(problems, fmt.Sprintf("missing required %q", key))
			missingRequired = append(missingRequired, key)
			continue
		}
		if isEmptyValue(v) {
			problems = append(problems, fmt.Sprintf("empty %q", key))
			emptyKeys[key] = true
		}
	}

	// wrong-type check on present keys.
	var unknownKeys []string
	for key, raw := range input {
		if strings.HasPrefix(key, "_") {
			continue // _parse_error, _raw_args, etc.
		}
		propRaw, ok := props[key]
		if !ok {
			unknownKeys = append(unknownKeys, key) // tolerate; maybe hint below
			continue
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
		if s, ok := raw.(string); ok && !emptyKeys[key] {
			if n := IntArg(propMap, "minLength", 0); len(strings.TrimSpace(s)) < n {
				problems = append(problems, fmt.Sprintf("%q too short: needs at least %d non-blank char(s)", key, n))
			}
		}
	}

	// near-miss key hint: when the model supplied an unknown key AND a required
	// key is missing, it likely used the wrong name (cmd->command). only fires
	// alongside an existing failure, so it never causes a new rejection.
	if len(missingRequired) > 0 && len(unknownKeys) > 0 {
		sort.Strings(unknownKeys) // deterministic hint order
		for _, uk := range unknownKeys {
			if best, ok := nearestKey(uk, missingRequired); ok {
				problems = append(problems, fmt.Sprintf("unknown key %q — did you mean %q?", uk, best))
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%s", formatSchemaError(spec, props, required, problems))
}

// nearestKey returns the closest candidate to key, used only to hint a likely
// typo/alias. Three cheap signals: substring containment (file->filepath),
// abbreviation (cmd->command: same first char, subsequence), and edit distance
// <= 2 for typos (commnd->command). Returns the best candidate and whether one
// qualified.
func nearestKey(key string, candidates []string) (string, bool) {
	lk := strings.ToLower(key)
	best := ""
	bestDist := 3 // accept <= 2
	for _, c := range candidates {
		lc := strings.ToLower(c)
		if strings.Contains(lc, lk) || strings.Contains(lk, lc) || abbrevMatch(lk, lc) {
			return c, true
		}
		if d := levenshtein(lk, lc); d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best, best != ""
}

// abbrevMatch reports whether a and b look like an abbreviation pair (cmd /
// command): same first char and the shorter is a subsequence of the longer.
// Requiring the shared first char and length >= 2 keeps it from firing on
// unrelated short keys (cwd vs command).
func abbrevMatch(a, b string) bool {
	if a == "" || b == "" || a[0] != b[0] {
		return false
	}
	short, long := a, b
	if len(long) < len(short) {
		short, long = long, short
	}
	if len(short) < 2 {
		return false
	}
	i := 0
	for j := 0; i < len(short) && j < len(long); j++ {
		if short[i] == long[j] {
			i++
		}
	}
	return i == len(short)
}

// levenshtein is the standard edit distance over bytes — tool-arg keys are
// ASCII, so byte-wise is correct and cheap.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(cur[j-1]+1, min(prev[j]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

// schemaRequired pulls the required slice from a schema regardless of whether
// it deserialized as []string or []any (json.Unmarshal yields []any for raw
// JSON, but hand-coded Go schemas often use []string).
func schemaRequired(schema map[string]any) []string {
	switch rs := schema["required"].(type) {
	case []string:
		return rs
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

// jsonTypeMismatch returns (gotName, true) when raw matches the declared JSON
// Schema type, or (gotName, false) otherwise. gotName is the JSON-ish label
// for the actual value, used in the diagnostic. Unknown schema types match.
func jsonTypeMismatch(raw any, want string) (string, bool) {
	got := jsonTypeName(raw)
	switch want {
	case "string", "number", "boolean", "array", "object":
		return got, got == want
	case "integer":
		f, isFloat := raw.(float64)
		return got, got == "number" && (!isFloat || f == float64(int64(f)))
	}
	return got, true
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
