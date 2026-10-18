package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/elhenro/bee/internal/safety"
)

// FilterPath returns abs relative to the workspace (root, or the working dir
// when root is empty), slash-separated. ok=false when abs lies outside it. One
// form for every mutation tool so a write filter sees the same string no
// matter how the model spelled the path.
func FilterPath(root, abs string) (string, bool) {
	base := root
	if base == "" {
		var err error
		if base, err = os.Getwd(); err != nil {
			return "", false
		}
	}
	rel, err := filepath.Rel(resolveSymlinks(base), resolveSymlinks(abs))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// CheckWriteTarget runs the per-write guards on an already-resolved abs path.
// Under a confined root it refuses sensitive targets, checking both the path
// as given and its symlink-resolved target (a link inside root to a hook file
// must not pass), and returns the resolved path so the caller writes exactly
// what was checked. In danger-full-access (empty root) the sensitive check is
// skipped and abs comes back unchanged. pathRe, when set, applies in both
// modes against FilterPath. denied is "" when allowed, else a message for the
// model.
func CheckWriteTarget(root, abs string, pathRe *regexp.Regexp) (target, denied string) {
	target = abs
	if root != "" {
		target = resolveSymlinks(abs)
		for _, p := range []string{abs, target} {
			if err := safety.CheckWritable(p); err != nil {
				return abs, fmt.Sprintf("path %q refused: %v", abs, err)
			}
		}
	}
	if pathRe == nil {
		return target, ""
	}
	rel, ok := FilterPath(root, abs)
	if !ok {
		return abs, fmt.Sprintf("path %q denied by write filter (outside workspace)", abs)
	}
	if !pathRe.MatchString(rel) {
		return abs, fmt.Sprintf("path %q denied by write filter", rel)
	}
	return target, ""
}
