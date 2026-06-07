package waggle

import "strings"

// shellCommand renders one recorded call as a deterministic bash command line.
// paramTok(step, key) returns the positional token (e.g. "$1") for a varying
// argument, or "" when the argument is a literal. ok=false means the tool has no
// safe shell translation, so the route must not be crystallized.
//
// Secondary flags (grep glob/context/count_only, read offset/limit) are dropped:
// the route is reproduced approximately, which is the accepted tradeoff for a
// read-only replay. bash calls run verbatim; a parameterized raw bash command is
// not supported (its value cannot be safely re-injected as a positional arg).
func shellCommand(step int, c Call, paramTok func(step int, key string) string) (string, bool) {
	tokenFor := func(key string) string {
		if t := paramTok(step, key); t != "" {
			return `"` + t + `"`
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
		if paramTok(step, "command") != "" {
			return "", false
		}
		cmd := strings.TrimSpace(c.Args["command"])
		if cmd == "" {
			return "", false
		}
		return cmd, true
	case "grep":
		if _, ok := c.Args["pattern"]; !ok {
			return "", false
		}
		return "grep -rn " + tokenFor("pattern") + " " + pathArg(), true
	case "read":
		if _, ok := c.Args["path"]; !ok {
			return "", false
		}
		return "cat " + tokenFor("path"), true
	case "ls":
		return "ls " + pathArg(), true
	case "find":
		if _, ok := c.Args["pattern"]; !ok {
			return "", false
		}
		return "find " + pathArg() + " -name " + tokenFor("pattern"), true
	}
	return "", false
}

// singleQuote wraps s in single quotes for safe literal inclusion, escaping
// embedded single quotes via the standard '\” dance.
func singleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
