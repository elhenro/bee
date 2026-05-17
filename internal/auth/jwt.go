package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// ExtractClaim decodes the unverified payload of a JWT and returns the value
// of the named claim, or "" if absent/non-string.
//
// path supports two forms:
//   - "foo"                          → top-level "foo"
//   - "https://example.com/x.bar"    → nested: first segment is the FULL claim
//     name (URI claims allowed), subsequent dot-separated segments traverse
//     into the nested object.
//
// No signature verification. Caller must ensure the JWT came from a trusted
// source (e.g. just-completed TLS-secured OAuth exchange).
func ExtractClaim(jwt, path string) string {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// try standard base64 in case of padding
		raw, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return lookupPath(payload, path)
}

func lookupPath(obj map[string]any, path string) string {
	// URI claim with dotted suffix: split at the LAST dot whose right side
	// starts with a non-URI character. Heuristic: try the whole path as a
	// top-level key first; if not found, peel from the right.
	if v, ok := obj[path].(string); ok {
		return v
	}
	// Iterate split points right-to-left.
	for i := len(path) - 1; i > 0; i-- {
		if path[i] != '.' {
			continue
		}
		head := path[:i]
		tail := path[i+1:]
		if inner, ok := obj[head].(map[string]any); ok {
			return lookupPath(inner, tail)
		}
	}
	return ""
}
