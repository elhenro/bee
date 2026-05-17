// Recursive @path import expansion for AGENTS.md / CLAUDE.md context files.
//
// `@./notes.md` or `@~/global.md` inside a context file gets replaced with
// the imported file's body inline. Depth-capped, cycle-guarded, with
// fzf-style fuzzy fallback when the exact path misses (so `@plan.md`
// resolves to `PLAN.md`).
package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sahilm/fuzzy"
)

const (
	maxImportDepth = 5
	maxFuzzyWalk   = 2000
)

// importPattern matches `@<path>` tokens. Leading char must be start-of-string
// or whitespace/punctuation (so `user@example.com` does not match). Allows
// optional `~/` or `/` prefix in addition to relative paths.
var importPattern = regexp.MustCompile(`(^|[\s(\[{"'` + "`" + `])@((?:~/|/)?[A-Za-z0-9_./-]+)`)

// noiseDirs are skipped by the fuzzy walker.
var noiseDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".bee": true,
}

// expandContextImports inlines @-imports in body. Recurses to maxImportDepth.
// visited tracks already-imported paths to break cycles. baseDir resolves
// relative imports (typically the dir containing the file).
func expandContextImports(body, baseDir string, visited map[string]bool, depth int) string {
	if depth >= maxImportDepth || !strings.Contains(body, "@") {
		return body
	}
	return importPattern.ReplaceAllStringFunc(body, func(match string) string {
		groups := importPattern.FindStringSubmatch(match)
		if len(groups) < 3 {
			return match
		}
		prefix, ref := groups[1], groups[2]
		path, content, ok := resolveImport(baseDir, ref)
		if !ok || visited[path] {
			return match
		}
		visited[path] = true
		nested := expandContextImports(content, filepath.Dir(path), visited, depth+1)
		return prefix + fmt.Sprintf("<import path=%q>\n%s\n</import>",
			path, strings.TrimRight(nested, "\n"))
	})
}

// resolveImport tries exact path first (with ~ expansion), falls back to
// fuzzy search within baseDir. Returns absolute resolved path + body.
func resolveImport(baseDir, ref string) (string, string, bool) {
	if strings.HasPrefix(ref, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			ref = filepath.Join(home, ref[2:])
		}
	}
	var exact string
	if filepath.IsAbs(ref) {
		exact = ref
	} else {
		exact = filepath.Join(baseDir, ref)
	}
	if data, ok := readImport(exact); ok {
		abs, _ := filepath.Abs(exact)
		return abs, data, true
	}
	// fuzzy fallback only for relative refs — absolute paths are explicit.
	if filepath.IsAbs(ref) {
		return "", "", false
	}
	return fuzzyResolve(baseDir, ref)
}

// readImport reads path with size cap + binary skip. Empty body returns false.
func readImport(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil || isBinaryBytes(data) {
		return "", false
	}
	if len(data) > MaxFileBytes {
		data = data[:MaxFileBytes]
	}
	return string(data), true
}

// fuzzyResolve walks baseDir (skipping noise dirs), runs fuzzy.Find, returns
// the best match's path + body if readable.
func fuzzyResolve(baseDir, needle string) (string, string, bool) {
	paths := walkFuzzy(baseDir)
	if len(paths) == 0 {
		return "", "", false
	}
	matches := fuzzy.Find(needle, paths)
	if len(matches) == 0 {
		return "", "", false
	}
	best := filepath.Join(baseDir, matches[0].Str)
	if data, ok := readImport(best); ok {
		abs, _ := filepath.Abs(best)
		return abs, data, true
	}
	return "", "", false
}

func walkFuzzy(root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if noiseDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, rel)
		if len(out) >= maxFuzzyWalk {
			return filepath.SkipAll
		}
		return nil
	})
	return out
}
