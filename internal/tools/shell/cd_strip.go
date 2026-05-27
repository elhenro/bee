package shell

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// cdPrefixRe matches `cd <path> && <rest>` and `cd <path>; <rest>`. Path is
// either bare (no spaces / metachars) or single/double quoted. Captures: 1=path
// (unquoted form), 2=quoted path body, 3=quoted path body, 4=separator, 5=rest.
//
// Deliberately conservative on the path: rejects globs, vars, command subst
// so we don't strip a dynamic cd target the model meant to keep.
var cdPrefixRe = regexp.MustCompile(`^\s*cd\s+(?:([^\s;&|"'$` + "`" + `*?<>]+)|"([^"$` + "`" + `]*)"|'([^']*)')\s*(&&|;)\s*(.+)$`)

// stripRedundantCd removes a leading `cd <path> && rest` from cmd when path
// resolves to the bee process cwd. Returns (newCmd, note) — note is a
// one-line breadcrumb appended to tool output so the model learns to omit
// the prefix next time. When no strip applies, returns (cmd, "").
//
// Tilde expansion (`~`, `~/foo`) is handled so `cd ~/projects/bee && ...`
// can be detected. $VAR expansion is intentionally NOT done — those are
// dynamic and stripping would change semantics.
func stripRedundantCd(cmd string) (string, string) {
	m := cdPrefixRe.FindStringSubmatch(cmd)
	if m == nil {
		return cmd, ""
	}
	var path string
	switch {
	case m[1] != "":
		path = m[1]
	case m[2] != "":
		path = m[2]
	case m[3] != "":
		path = m[3]
	}
	rest := strings.TrimSpace(m[5])
	if rest == "" {
		return cmd, ""
	}
	resolved, ok := resolveCdTarget(path)
	if !ok {
		return cmd, ""
	}
	procCwd, err := os.Getwd()
	if err != nil {
		return cmd, ""
	}
	if !samePath(resolved, procCwd) {
		// different dir → legitimate, keep as-is.
		return cmd, ""
	}
	note := "[note] stripped redundant `cd " + path + " && ` — bee already runs in this dir."
	return rest, note
}

// resolveCdTarget expands ~ and makes path absolute when possible. Returns
// (resolved, true) on success. Bare `~` and `~/<rest>` are expanded against
// $HOME; absolute paths pass through; relative paths skip (we can't tell
// without joining against an assumed base, and a relative `cd foo` is
// almost certainly a real dir change anyway).
func resolveCdTarget(p string) (string, bool) {
	if p == "" {
		return "", false
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		if p == "~" {
			return home, true
		}
		return filepath.Join(home, p[2:]), true
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), true
	}
	return "", false
}

// samePath compares two filesystem paths after cleaning + resolving symlinks.
// EvalSymlinks failure (e.g. transient stat error) falls back to a Clean
// comparison so we don't accidentally keep a redundant cd just because a
// symlink resolution flaked.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 == nil && err2 == nil {
		return ra == rb
	}
	return false
}
