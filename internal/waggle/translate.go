package waggle

import "strings"

// shellCommand renders one recorded call as a deterministic bash command line.
// paramTok(step, key) returns the fully rendered token for a varying argument
// (an exec waggle passes "\"$1\"", a recipe passes {{argv.0}} which the
// recipe runtime quotes itself), or "" when the argument is a literal. ok=false means the tool has no
// safe shell translation, so the route must not be crystallized.
//
// Secondary flags (grep glob/context/count_only, read offset/limit) are dropped:
// the route is reproduced approximately, which is the accepted tradeoff for a
// read-only replay. bash calls run verbatim; a parameterized raw bash command is
// not supported (its value cannot be safely re-injected as a positional arg).
func shellCommand(step int, c Call, paramTok func(step int, key string) string) (string, bool) {
	tokenFor := func(key string) string {
		if t := paramTok(step, key); t != "" {
			return t
		}
		return singleQuote(c.Args[key])
	}
	pathArg := func() string {
		if _, ok := c.Args["path"]; ok {
			return tokenFor("path")
		}
		return "."
	}
	switch c.Tool {
	case "bash":
		// never translate a recorded bash step into a replayable command: a route
		// loaded from disk could otherwise carry an arbitrary shell command that
		// auto-fires without approval. Mined routes exclude bash (it's mutating),
		// so dropping this loses nothing legitimate.
		return "", false
	case "search": // bee's grep tool is registered as "search"
		if _, ok := c.Args["pattern"]; !ok {
			return "", false
		}
		// -e and -- keep a dash-leading pattern or path from parsing as a flag
		return "grep -rn -e " + tokenFor("pattern") + " -- " + pathArg(), true
	case "read":
		if _, ok := c.Args["path"]; !ok {
			return "", false
		}
		return "cat -- " + tokenFor("path"), true
	case "ls":
		return "ls -- " + pathArg(), true
	case "glob": // bee's find tool is registered as "glob"
		if _, ok := c.Args["pattern"]; !ok {
			return "", false
		}
		// find has no portable --: a literal start point that parses as an
		// expression is refused, a parameterized one is checked at run time
		if _, ok := c.Args["path"]; ok && paramTok(step, "path") == "" && exprLike(c.Args["path"]) {
			return "", false
		}
		return "find " + pathArg() + " -name " + tokenFor("pattern"), true
	}
	return "", false
}

// exprLike reports whether s would be read by find as an expression, not a
// start point.
func exprLike(s string) bool {
	return strings.HasPrefix(s, "-") || strings.HasPrefix(s, "!") || strings.HasPrefix(s, "(")
}

// singleQuote wraps s in single quotes for safe literal inclusion, escaping
// embedded single quotes via the standard '\” dance.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
