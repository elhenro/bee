package tools

import (
	"strconv"
	"strings"
)

// IntArg returns the int value at key, accepting JSON number variants and
// stringified ints (provider quirk). Returns def when missing or unparseable.
func IntArg(in map[string]any, key string, def int) int {
	v, ok := in[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return def
		}
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return def
}
