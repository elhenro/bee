// Inline @path file expansion for user prompts.
//
// Tokens like `@cmd/bee/main.go` in a user message get replaced with the
// file contents wrapped in a fenced block. Caveats: paths must exist
// relative to cwd, binaries are skipped, files larger than MaxFileBytes
// truncate.
package prompt

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// MaxFileBytes caps each expanded file to keep prompts bounded.
const MaxFileBytes = 64 * 1024

// atPathPattern matches `@<rel-path>` where the leading char is start-of-string
// or whitespace/punctuation — so `user@example.com` does NOT match.
// Path chars: letters, digits, _, /, ., -. Stops at whitespace, comma, etc.
var atPathPattern = regexp.MustCompile(`(^|[\s(\[{"'` + "`" + `])@([A-Za-z0-9_./-][A-Za-z0-9_./-]*)`)

// ExpandAtPaths replaces every `@<rel-path>` token in text with a fenced
// block containing the file's contents. Failed lookups, binaries, and
// missing files are left as-is — the model still sees the literal `@path`
// and can react. Returns the rewritten text.
func ExpandAtPaths(text, cwd string) string {
	if !strings.Contains(text, "@") {
		return text
	}
	return atPathPattern.ReplaceAllStringFunc(text, func(match string) string {
		groups := atPathPattern.FindStringSubmatch(match)
		if len(groups) < 3 {
			return match
		}
		prefix := groups[1]
		// sentence punctuation after a path ("@main.go.") is not part of it
		rel := strings.TrimRight(groups[2], ".,;:")
		trailing := groups[2][len(rel):]
		expanded, ok := readForInline(cwd, rel)
		if !ok {
			return match
		}
		return prefix + expanded + trailing
	})
}

// readForInline resolves rel against cwd, refuses absolute escapes,
// skips binary, applies the size cap. Returns "" + false on any failure.
func readForInline(cwd, rel string) (string, bool) {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return "", false
	}
	abs := filepath.Join(cwd, filepath.Clean(rel))
	// keep us inside cwd — Clean + Join handles `..` but double-check.
	if !strings.HasPrefix(abs, filepath.Clean(cwd)+string(filepath.Separator)) && abs != filepath.Clean(cwd) {
		return "", false
	}
	data, truncated, ok := readCapped(abs)
	if !ok {
		return "", false
	}
	out := fmt.Sprintf("### @%s\n```\n%s\n```", rel, strings.TrimRight(string(data), "\n"))
	if truncated {
		out += fmt.Sprintf("\n(truncated at %d bytes)", MaxFileBytes)
	}
	return out, true
}

// readCapped reads a regular text file up to MaxFileBytes; truncated reports
// a longer file. Directories and binaries fail.
func readCapped(path string) (data []byte, truncated, ok bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, false, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, false
	}
	defer f.Close()
	data, err = io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil || isBinaryBytes(data) {
		return nil, false, false
	}
	if len(data) > MaxFileBytes {
		return data[:MaxFileBytes], true, true
	}
	return data, false, true
}

// isBinaryBytes mirrors the heuristic in tools/read/read.go without
// pulling in a circular dep. NUL byte OR >30% non-text in first 4KB.
func isBinaryBytes(buf []byte) bool {
	buf = buf[:min(len(buf), 4096)]
	if len(buf) == 0 {
		return false
	}
	nonText := 0
	for _, b := range buf {
		switch {
		case b == 0:
			return true
		case b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\b':
		case b < 0x20 || b == 0x7f:
			nonText++
		}
	}
	return nonText*100/len(buf) > 30
}
