package tools

import (
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
