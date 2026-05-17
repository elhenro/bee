package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	b, err := json.Marshal(shortenPathishValues(in))
	if err != nil {
		return "{...}"
	}
	return truncateRunes(string(b), budget)
}

// shortenPathishValues returns a shallow copy of in with values for path-ish
// keys collapsed to cwd-relative / ~-prefixed form. Used in the multi-key
// summary path so grep-style `{glob, path}` calls don't blow tool-card width
// with /Users/<name>/projects/<repo>/... repeated on every render.
func shortenPathishValues(in map[string]any) map[string]any {
	pathish := map[string]bool{"path": true, "file_path": true}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if pathish[k] {
			if s, ok := v.(string); ok {
				out[k] = shortenPath(s)
				continue
			}
		}
		out[k] = v
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
	primary := map[string]bool{
		"path": true, "pattern": true, "cmd": true, "command": true,
		"name": true, "query": true, "file_path": true, "regex": true,
	}
	pathish := map[string]bool{"path": true, "file_path": true}
	// pathBearing keys hold free-form text that may *embed* absolute paths
	// (e.g. `cmd: cd /Users/.../bee && go test`). Inline-shorten those so
	// the tool card doesn't waste 30 chars on the home prefix every render.
	pathBearing := map[string]bool{"cmd": true, "command": true}
	for k, v := range in {
		if !primary[k] {
			return "", false
		}
		switch x := v.(type) {
		case string:
			val := x
			switch {
			case pathish[k]:
				val = shortenPath(val)
			case pathBearing[k]:
				val = shortenPathsInline(val)
			}
			return k + ": " + val, true
		default:
			b, err := json.Marshal(x)
			if err != nil {
				return "", false
			}
			return k + ": " + string(b), true
		}
	}
	return "", false
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
		s = strings.ReplaceAll(s, cachedCwd, ".")
	}
	if cachedHome != "" {
		s = strings.ReplaceAll(s, cachedHome+string(filepath.Separator), "~"+string(filepath.Separator))
		s = strings.ReplaceAll(s, cachedHome, "~")
	}
	return s
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
