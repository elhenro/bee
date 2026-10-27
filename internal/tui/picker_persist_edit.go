package tui

import (
	"bytes"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// patchTOML applies top-level key assignments to the config text in old.
// Scalar values are edited line by line so comments, key order and formatting
// survive. Anything else (arrays, structs, an unusual layout) falls back to
// re-encoding the parsed tree, which drops comments. The in-place result is
// parsed back and compared to the tree encoding before it is trusted, so a
// bad line edit can never reach disk. Errors only when old does not parse.
func patchTOML(old []byte, set map[string]any) ([]byte, error) {
	tree := map[string]any{}
	if err := toml.Unmarshal(old, &tree); err != nil {
		return nil, err
	}
	maps.Copy(tree, set)
	want, err := toml.Marshal(tree)
	if err != nil {
		return nil, err
	}
	if out, ok := editInPlace(string(old), set); ok {
		var got map[string]any
		if toml.Unmarshal(out, &got) == nil {
			if enc, err := toml.Marshal(got); err == nil && bytes.Equal(enc, want) {
				return out, nil
			}
		}
	}
	return want, nil
}

func editInPlace(text string, set map[string]any) ([]byte, bool) {
	var lines []string
	if text != "" {
		lines = strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	for _, k := range slices.Sorted(maps.Keys(set)) {
		val, ok := scalarText(set[k])
		if !ok {
			return nil, false
		}
		if lines, ok = setLine(lines, k, val); !ok {
			return nil, false
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n"), true
}

// scalarText encodes a bool, int or string as a one-line toml value.
func scalarText(v any) (string, bool) {
	switch v.(type) {
	case bool, int, int64, string:
	default:
		return "", false
	}
	b, err := toml.Marshal(map[string]any{"k": v})
	s, ok := strings.CutPrefix(strings.TrimSuffix(string(b), "\n"), "k = ")
	return s, err == nil && ok && !strings.Contains(s, "\n")
}

// setLine rewrites the value of top-level key, or inserts the assignment after
// the last top-level line (before the first table, keeping that table's own
// comment attached to it).
func setLine(lines []string, key, val string) ([]string, bool) {
	assign := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)(.*)$`)
	hdr := len(lines)
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[") {
			hdr = i
			break
		}
		if m := assign.FindStringSubmatch(l); m != nil {
			rest, ok := afterValue(m[2])
			if !ok {
				return nil, false
			}
			lines[i] = m[1] + val + rest
			return lines, true
		}
	}
	p := hdr
	for hdr < len(lines) && p > 0 && strings.HasPrefix(strings.TrimSpace(lines[p-1]), "#") {
		p--
	}
	for p > 0 && strings.TrimSpace(lines[p-1]) == "" {
		p--
	}
	return slices.Insert(lines, p, key+" = "+val), true
}

// afterValue returns what follows a single-line scalar value (spacing and any
// trailing comment). ok is false for values it cannot delimit safely.
func afterValue(v string) (rest string, ok bool) {
	switch {
	case strings.HasPrefix(v, `"""`), strings.HasPrefix(v, `'''`), strings.HasPrefix(v, "["), strings.HasPrefix(v, "{"):
		return "", false
	case strings.HasPrefix(v, `"`):
		for i := 1; i < len(v); i++ {
			switch v[i] {
			case '\\':
				i++
			case '"':
				return v[i+1:], true
			}
		}
		return "", false
	case strings.HasPrefix(v, "'"):
		i := strings.IndexByte(v[1:], '\'')
		return v[i+2:], i >= 0
	}
	cut := len(v)
	if i := strings.IndexByte(v, '#'); i >= 0 {
		cut = i
	}
	return v[len(strings.TrimRight(v[:cut], " \t\r")):], true
}
