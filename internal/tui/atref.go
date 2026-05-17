package tui

import (
	"os"
	"path/filepath"
	"strings"
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
			p = p[:len(p)-1]
			if p == "" {
				return ""
			}
		}
	}
	return p
}

// fuzzyMax caps the fuzzy file walker so the @-picker stays snappy.
const fuzzyMax = 50

// FuzzyFiles returns paths under root containing needle (case-insensitive).
// Skips .git, node_modules, vendor. Caps at fuzzyMax results.
func FuzzyFiles(root, needle string) []string {
	needleLower := strings.ToLower(needle)
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if n == ".git" || n == "node_modules" || n == "vendor" {
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
