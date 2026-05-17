// Inline @path file expansion for user prompts.
//
// Tokens like `@cmd/bee/main.go` in a user message get replaced with the
// file contents wrapped in a fenced block. Caveats: paths must exist
// relative to cwd, binaries are skipped, files larger than MaxFileBytes
// truncate.
package prompt

import (
	"fmt"
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
		rel := groups[2]
		expanded, ok := readForInline(cwd, rel)
		if !ok {
			return match
		}
		return prefix + expanded
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
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return "", false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", false
	}
	if isBinaryBytes(data) {
		return "", false
	}
	truncated := false
	if len(data) > MaxFileBytes {
		data = data[:MaxFileBytes]
		truncated = true
	}
	body := string(data)
	body = strings.TrimRight(body, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "### @%s\n```\n%s\n```", rel, body)
	if truncated {
		fmt.Fprintf(&b, "\n(truncated at %d bytes)", MaxFileBytes)
	}
	return b.String(), true
}

// isBinaryBytes mirrors the heuristic in tools/read/read.go without
// pulling in a circular dep. NUL byte OR >30% non-text in first 4KB.
func isBinaryBytes(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	if len(buf) > 4096 {
		buf = buf[:4096]
	}
	for _, b := range buf {
		if b == 0 {
			return true
		}
	}
	nonText := 0
	for _, b := range buf {
		if b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\b' {
			continue
		}
		if b < 0x20 || b == 0x7f {
			nonText++
		}
	}
	return nonText*100/len(buf) > 30
}
