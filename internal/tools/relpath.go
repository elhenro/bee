package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// RelTo returns p relative to base. Falls back to absolute path on cross-tree
// or error so callers never get an empty/wrong path.
func RelTo(base, p string) string {
	if base == "" {
		return p
	}
	rel, err := filepath.Rel(base, p)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return p
	}
	return rel
}

// expandHome expands a leading ~ or ~/ against $HOME. Other paths pass through.
func expandHome(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// resolveSymlinks resolves symlinks in path even when path does not exist yet
// (e.g. a file about to be written). It resolves the longest existing ancestor
// and re-appends the missing trailing components. On any error it returns the
// cleaned input so callers degrade to a plain comparison.
func resolveSymlinks(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir, rest := filepath.Dir(path), filepath.Base(path)
	for dir != path { // walk up until something resolves or we hit the root
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			return filepath.Join(resolved, rest)
		}
		rest = filepath.Join(filepath.Base(dir), rest)
		path, dir = dir, filepath.Dir(dir)
	}
	return filepath.Clean(path)
}

// ResolveInRoot resolves path against workspace root and verifies containment.
// It expands a leading ~, makes relative paths absolute under root, and
// resolves symlinks on both sides so a symlinked workspace root (and absolute
// inputs pointing at the symlink target) are treated as inside the sandbox.
// Returns the resolved absolute path, its path relative to root, the resolved
// root, and ok=false when path escapes the root.
func ResolveInRoot(root, path string) (abs, rel, rootAbs string, ok bool) {
	path = expandHome(path)
	abs = path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, path)
	}
	abs = filepath.Clean(abs)

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return abs, "", root, false
	}

	rRoot := resolveSymlinks(rootAbs)
	rAbs := resolveSymlinks(abs)
	rel, err = filepath.Rel(rRoot, rAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs, "", rootAbs, false
	}
	return abs, rel, rootAbs, true
}
