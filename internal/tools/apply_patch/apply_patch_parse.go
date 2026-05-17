package apply_patch

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// hunkHeaderRe matches `@@ -a[,b] +c[,d] @@[ section]`.
var hunkHeaderRe = regexp.MustCompile(`^(@@ -)(\d+)(?:,\d+)?( \+)(\d+)(?:,\d+)?( @@.*)$`)

// isHunkCountErr returns true when the parse error is the go-gitdiff
// "fragment header miscounts lines" complaint we know how to repair.
func isHunkCountErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "miscounts lines")
}

// repairHunkCounts rewrites the line-count fields of every `@@ ... @@` header
// to match the actual body that follows. Body lines: ` ` = context (counts
// in both), `-` = old only, `+` = new only. Empty lines are treated as
// stripped-trailing-space context (counts in both) which matches go-gitdiff.
// File-marker lines (`diff `, `--- `, `+++ `, next `@@`) end the hunk.
func repairHunkCounts(patch string) string {
	lines := strings.Split(patch, "\n")
	for i, ln := range lines {
		m := hunkHeaderRe.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		// locate body end: next file marker / hunk header
		end := i + 1
		for end < len(lines) {
			b := lines[end]
			if strings.HasPrefix(b, "@@") ||
				strings.HasPrefix(b, "diff ") ||
				strings.HasPrefix(b, "--- ") ||
				strings.HasPrefix(b, "+++ ") {
				break
			}
			end++
		}
		// trim trailing empties (strings.Split adds one for final \n)
		for end > i+1 && lines[end-1] == "" {
			end--
		}
		oldN, newN := 0, 0
		for j := i + 1; j < end; j++ {
			b := lines[j]
			switch {
			case strings.HasPrefix(b, "+"):
				newN++
			case strings.HasPrefix(b, "-"):
				oldN++
			case strings.HasPrefix(b, "\\"):
				// "\ No newline at end of file" — skip
			default:
				// context (incl. mid-body empty = stripped trailing-space line)
				oldN++
				newN++
			}
		}
		lines[i] = fmt.Sprintf("%s%s,%d%s%s,%d%s", m[1], m[2], oldN, m[3], m[4], newN, m[5])
	}
	return strings.Join(lines, "\n")
}

// stripDiffPrefix drops git's a/ or b/ leading segment when present.
func stripDiffPrefix(p string) string {
	if p == "" || p == "/dev/null" || filepath.IsAbs(p) {
		return p
	}
	switch {
	case strings.HasPrefix(p, "a/"):
		return p[2:]
	case strings.HasPrefix(p, "b/"):
		return p[2:]
	}
	return p
}
