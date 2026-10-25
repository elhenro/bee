package tui

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// summarizeArgs renders the tool input map as a single-line, truncated
// string. When the input has a single primary key (path/pattern/cmd/name/
// query/file_path/regex) the JSON wrapping is dropped and just the value is
// shown — `pattern: foo` rather than `{"pattern":"foo"}`. Falls back to
// compact JSON for everything else.
func summarizeArgs(in map[string]any, budget int) string {
	if len(in) == 0 {
		return ""
	}
	if s, ok := summarizeSingleKey(in); ok {
		return truncateRunes(s, budget)
	}
	return truncateRunes(formatKV(shortenPathishValues(in)), budget)
}

// formatKV renders a tool input as `key=value key=value`, keys sorted so the
// row is stable across renders. Strings with spaces are quoted; everything
// else goes through json so booleans, numbers and nested values stay legible.
func formatKV(in map[string]any) string {
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(in)) {
		v, isStr := in[k].(string)
		switch {
		case isStr && strings.ContainsAny(v, " \t"):
			v = strconv.Quote(v)
		case !isStr:
			if b, err := json.Marshal(in[k]); err != nil {
				v = "?"
			} else {
				v = string(b)
			}
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, " ")
}

// shortenPathishValues returns a shallow copy of in with values for path-ish
// keys collapsed to cwd-relative / ~-prefixed form. Used in the multi-key
// summary path so grep-style `{glob, path}` calls don't blow tool-card width
// with /Users/<name>/projects/<repo>/... repeated on every render.
func shortenPathishValues(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
		if s, ok := v.(string); ok && (k == "path" || k == "file_path") {
			out[k] = shortenPath(s)
		}
	}
	return out
}

// summarizeSingleKey returns a `key: value` summary when in has exactly one
// entry whose key is one of the well-known primaries. Strings render
// unquoted; other scalars use json so booleans/numbers stay legible.
// Path-shaped keys get cwd/home prefix stripping so tool cards don't waste
// width on `/Users/<name>/projects/<repo>/` repeated on every read call.
func summarizeSingleKey(in map[string]any) (string, bool) {
	if len(in) != 1 {
		return "", false
	}
	for k, v := range in {
		if !primaryArgKeys[k] {
			return "", false
		}
		s, isStr := v.(string)
		if !isStr {
			b, err := json.Marshal(v)
			return string(b), err == nil
		}
		switch k {
		case "path", "file_path":
			return shortenPath(s), true
		case "cmd", "command":
			// free-form text that may *embed* absolute paths (e.g. `cmd: cd
			// /Users/.../bee && go test`): inline-shorten so the tool card
			// doesn't waste 30 chars on the home prefix every render
			return shortenPathsInline(s), true
		}
		return s, true
	}
	return "", false
}

var primaryArgKeys = map[string]bool{
	"path": true, "pattern": true, "cmd": true, "command": true,
	"name": true, "query": true, "file_path": true, "regex": true,
}

// pathRoots caches cwd + home once so summarizeArgs stays allocation-light
// across the bursty tool-call render path. getwd/UserHomeDir hit syscalls
// on every call otherwise.
var (
	pathRootsOnce sync.Once
	cachedCwd     string
	cachedHome    string
)

func initPathRoots() {
	if d, err := os.Getwd(); err == nil {
		cachedCwd = filepath.Clean(d)
	}
	if h, err := os.UserHomeDir(); err == nil {
		cachedHome = filepath.Clean(h)
	}
}

// shortenPathsInline rewrites every absolute path token embedded inside s to
// its cwd-relative form (or `~/...` when only home matches). Used for free-
// form strings that contain paths — bash `cmd:` summaries (`cd /Users/x/
// projects/bee && go test` → `cd . && go test`) and raw tool stdout lines.
// Pure prefix-substring replace, cheap on the hot render path. Empty cwd /
// home (e.g. tests, root user) degrades to a no-op.
func shortenPathsInline(s string) string {
	if s == "" {
		return s
	}
	pathRootsOnce.Do(initPathRoots)
	if cachedCwd != "" {
		s = strings.ReplaceAll(s, cachedCwd+string(filepath.Separator), "")
		s = replaceRoot(s, cachedCwd, ".")
	}
	if cachedHome != "" {
		s = strings.ReplaceAll(s, cachedHome+string(filepath.Separator), "~"+string(filepath.Separator))
		s = replaceRoot(s, cachedHome, "~")
	}
	return s
}

// replaceRoot swaps root for repl where it ends a path element, so a sibling
// such as root-new keeps its name.
func replaceRoot(s, root, repl string) string {
	parts := strings.Split(s, root)
	out := parts[0]
	for _, p := range parts[1:] {
		r, _ := utf8.DecodeRuneInString(p)
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			out += root
		} else {
			out += repl
		}
		out += p
	}
	return out
}

// humanBytes formats n as a short decimal-prefixed size ("812 B", "2.4 KB",
// "1.1 MB"). Decimal (1000), not binary — reads like `ls -h` and aligns
// with token-cost intuition more than disk-block intuition.
func humanBytes(n int) string {
	const (
		kb = 1000
		mb = 1000 * 1000
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < mb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	}
}

// shortenPath collapses absolute paths under cwd → relative; otherwise
// under home → `~/...`. Non-absolute or out-of-tree paths pass through.
func shortenPath(p string) string {
	if p == "" || !filepath.IsAbs(p) {
		return p
	}
	pathRootsOnce.Do(initPathRoots)
	cleaned := filepath.Clean(p)
	if cachedCwd != "" {
		if rel, err := filepath.Rel(cachedCwd, cleaned); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "./"
			}
			return rel
		}
	}
	if cachedHome != "" && strings.HasPrefix(cleaned, cachedHome+string(filepath.Separator)) {
		return "~" + cleaned[len(cachedHome):]
	}
	return p
}

// truncateRunes caps a display string at n runes, suffixing an ellipsis when
// it had to cut. Operates on runes so multibyte content (paths with unicode)
// doesn't get sliced mid-codepoint.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n-1]) + "…"
}
