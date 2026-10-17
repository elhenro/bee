package recipe

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// placeholderRe matches {{name}}: args, argv.N, or an earlier step's id.
// Plain substitution rather than text/template so step ids may carry hyphens
// and a tiny model can author a recipe without learning template syntax.
var placeholderRe = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_][A-Za-z0-9_.-]*)\s*\}\}`)

// scope is what a template can reach: the invocation args and every step
// output produced so far.
type scope struct {
	argv    []string
	outputs map[string]string
}

func (sc scope) lookup(key string) (string, bool) {
	switch {
	case key == "args":
		return strings.Join(sc.argv, " "), true
	case strings.HasPrefix(key, "argv."):
		n, err := strconv.Atoi(strings.TrimPrefix(key, "argv."))
		if err != nil || n < 0 || n >= len(sc.argv) {
			return "", false
		}
		return sc.argv[n], true
	}
	v, ok := sc.outputs[key]
	return v, ok
}

// expand substitutes every placeholder in tmpl. quote, when non-nil, wraps
// each substituted value (shell steps pass shellQuote). An unknown placeholder
// is an error rather than an empty string: a missing step output silently
// becoming "" would run a different command than the author wrote.
func expand(tmpl string, sc scope, quote func(string) string) (string, error) {
	var firstErr error
	out := placeholderRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		key := placeholderRe.FindStringSubmatch(m)[1]
		v, ok := sc.lookup(key)
		if !ok {
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown placeholder {{%s}}", key)
			}
			return m
		}
		if quote != nil {
			return quote(v)
		}
		return v
	})
	return out, firstErr
}

// shellQuote single-quotes s for literal inclusion in a command line.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
