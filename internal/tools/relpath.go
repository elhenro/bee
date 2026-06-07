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
	if err != nil {
		return p
	}
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, "..") {
		return p
	}
	return rel
}

// isRooted reports whether p starts at a path root without naming a volume,
// e.g. "/tmp" or "\tmp". filepath.IsAbs returns false for these on Windows, so
// callers must special-case them to avoid joining absolute inputs under root.
func isRooted(p string) bool {
	return len(p) > 0 && (p[0] == '/' || p[0] == '\\')
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

// pathIsInside checks whether absPath is inside rootPath, handling both
// forward- and backslash separators (important on Windows where
// filepath.Rel may return a path with / separators, and may omit the
// separator between ".." and the trailing component, e.g. "..tmp").
func pathIsInside(absPath, rootPath string) bool {
	return pathIsPrefix(absPath, rootPath)
}

// pathIsPrefix checks whether rootPath is a prefix of absPath using
// filepath.Clean to handle Windows path quirks (e.g. leading / on
// Windows resolves to cwd-relative, so we must compare against the
// cleaned absolute paths).
func pathIsPrefix(absPath, rootPath string) bool {
	absPath = filepath.Clean(absPath)
	rootPath = filepath.Clean(rootPath)
	if rootPath == "." {
		return true
	}
	// Normalize separators for comparison
	absPath = filepath.ToSlash(absPath)
	rootPath = filepath.ToSlash(rootPath)
	// The root contains itself.
	if absPath == rootPath {
		return true
	}
	// A child must sit under root with a separator boundary, so /x/001 is not
	// treated as containing /x/0011 and /tmp doesn't resolve inside /x/001.
	if !strings.HasSuffix(rootPath, "/") {
		rootPath += "/"
	}
	return strings.HasPrefix(absPath, rootPath)
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
	// isRooted catches volume-less rooted inputs ("/tmp", "\tmp"). On Windows
	// filepath.IsAbs is false for these (no drive letter), so without this they
	// get joined under root and wrongly pass the containment check. Treating
	// them as absolute makes them resolve to the drive root — outside the
	// sandbox — matching Unix, where "/tmp" already escapes.
	if !filepath.IsAbs(abs) && !isRooted(abs) {
		abs = filepath.Join(root, path)
	}
	abs = filepath.Clean(abs)

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return abs, "", root, false
	}

	rRoot := resolveSymlinks(rootAbs)
	rAbs := resolveSymlinks(abs)
	if !pathIsInside(rAbs, rRoot) {
		return abs, "", rootAbs, false
	}
	rel, err = filepath.Rel(rRoot, rAbs)
	if err != nil {
		return abs, "", rootAbs, false
	}
	return abs, rel, rootAbs, true
}
