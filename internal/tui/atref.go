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

// fuzzyMax caps the matches the @-picker lists so it stays snappy.
const fuzzyMax = 50

// indexMax bounds the file index so a huge tree cannot balloon memory.
const indexMax = 20000

// skipDirs are never indexed: dependencies, build output, worktree copies.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "worktrees": true,
	"target": true, "dist": true, "build": true, "__pycache__": true,
}

// fileIndex is a one-time walk of a tree. lower mirrors paths so each
// keystroke filters in memory without lowercasing or touching the disk.
type fileIndex struct {
	paths []string
	lower []string
}

// indexFiles walks root once, skipping hidden folders and skipDirs, and
// stops after indexMax files. Blocking: run it off the UI goroutine.
func indexFiles(root string) *fileIndex {
	idx := &fileIndex{}
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if p != root && (skipDirs[n] || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		idx.paths = append(idx.paths, rel)
		idx.lower = append(idx.lower, strings.ToLower(rel))
		if len(idx.paths) >= indexMax {
			return filepath.SkipAll
		}
		return nil
	})
	return idx
}

// filter returns up to fuzzyMax paths containing needle (case-insensitive),
// in walk order. A nil index matches nothing.
func (idx *fileIndex) filter(needle string) []string {
	if idx == nil {
		return nil
	}
	needle = strings.ToLower(needle)
	var out []string
	for i, l := range idx.lower {
		if strings.Contains(l, needle) {
			out = append(out, idx.paths[i])
			if len(out) >= fuzzyMax {
				break
			}
		}
	}
	return out
}
