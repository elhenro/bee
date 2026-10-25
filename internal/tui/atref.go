package tui

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// CompletionCandidates returns names in dir starting with prefix.
func CompletionCandidates(dir, prefix string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			out = append(out, e.Name())
		}
	}
	return out
}

// LongestCommonPrefix returns the longest string that is a prefix of every input.
func LongestCommonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			_, size := utf8.DecodeLastRuneInString(p)
			p = p[:len(p)-size]
			if p == "" {
				return ""
			}
		}
	}
	return p
}

// completePath extends the last word of val to the longest common prefix of
// the files it names under cwd. ok is false when nothing can be added.
func completePath(cwd, val string) (string, bool) {
	partial := val[strings.LastIndexAny(val, " \t")+1:]
	if partial == "" {
		return val, false
	}
	// Split, not Dir/Base: "src/" is the directory src with an empty prefix
	dir, base := filepath.Split(partial)
	switch {
	case dir == "":
		dir = cwd
	case !filepath.IsAbs(dir):
		dir = filepath.Join(cwd, dir)
	}
	cands := CompletionCandidates(dir, base)
	if len(cands) == 0 {
		return val, false
	}
	completion := LongestCommonPrefix(cands)
	if completion == base {
		return val, false
	}
	return val + completion[len(base):], true
}

// fuzzyMax caps the fuzzy file walker so the @-picker stays snappy.
const fuzzyMax = 50

// FuzzyFiles returns paths under root containing needle (case-insensitive).
// Skips hidden .folders, node_modules, vendor, worktrees. Caps at fuzzyMax results.
func FuzzyFiles(root, needle string) []string {
	needleLower := strings.ToLower(needle)
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if p != root && (n == "node_modules" || n == "vendor" || n == "worktrees" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if needleLower == "" || strings.Contains(strings.ToLower(rel), needleLower) {
			out = append(out, rel)
			if len(out) >= fuzzyMax {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return out
}
