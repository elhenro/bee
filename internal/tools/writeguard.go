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
// Under a confined root it refuses sensitive targets; in danger-full-access
// (empty root) that check is skipped. pathRe, when set, applies in both modes
// against FilterPath. Returns "" when allowed, else a denial message.
func CheckWriteTarget(root, abs string, pathRe *regexp.Regexp) string {
	if root != "" {
		if err := safety.CheckWritable(abs); err != nil {
			return fmt.Sprintf("path %q refused: %v", abs, err)
		}
	}
	if pathRe == nil {
		return ""
	}
	rel, ok := FilterPath(root, abs)
	if !ok {
		return fmt.Sprintf("path %q denied by write filter (outside workspace)", abs)
	}
	if !pathRe.MatchString(rel) {
		return fmt.Sprintf("path %q denied by write filter", rel)
	}
	return ""
}
