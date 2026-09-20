package loop

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// Error-family clustering: a model can be wedged on one failure *kind* while
// varying args just enough to dodge the identical-call and per-tool counters.
// This file derives a stable family key from a failing tool result so the loop
// can count consecutive failures of the same normalized error shape. Volatile
// detail — temp paths, line/column numbers, hex addresses, pids, timings,
// quoted paths — is scrubbed before hashing, so two messages that differ only
// in that detail collapse into one family.
//
// All work is regex-only against a single input string, so the derivation is
// deterministic across runs (no map iteration, no clock, no randomness).

// Normalization rules, applied in the order listed in normalizeErrorShape.
// Regex order is load-bearing: timings must be stripped before bare line/col
// numbers, and quoted paths before their unquoted remainder.
var (
	// ISO-ish timestamps, e.g. 2025-01-02T03:04:05Z.
	reTimestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}(?:[t ]\d{2}:\d{2}:\d{2}(?:\.\d+)?)?(?:z|[+-]\d{2}:?\d{2})?`)
	// bare clock times, e.g. 15:04:05.123.
	reClock = regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(?:\.\d+)?\b`)
	// durations / elapsed times, e.g. 0.01s, 12ms, 3m.
	reDuration = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ns|us|µs|ms|s|m|h)\b`)
	// hex addresses / memory offsets.
	reHexAddr = regexp.MustCompile(`0x[0-9a-f]+`)
	// quoted strings that contain a path separator ("a/b", 'a\b').
	reQuotedPath = regexp.MustCompile(`"[^"]*[/\\][^"]*"|'[^']*[/\\][^']*'`)
	// temp directories: macOS /var/folders, /private/var, /var/tmp and /tmp.
	reTempPath = regexp.MustCompile(`(?:/private)?/(?:var/folders|var/tmp|tmp)/[^\s'":;,)]*`)
	// absolute POSIX paths with two or more segments; the leading (^|non-path)
	// capture avoids eating the slashes of a URL scheme.
	reAbsPath = regexp.MustCompile(`(^|[^:/a-z0-9._@+-])(/[a-z0-9._@+-]+(?:/[a-z0-9._@+-]+)+)`)
	// pids in the several shapes tools emit them.
	rePID = regexp.MustCompile(`\bpid[ =:#]*\d+\b`)
	// "line 12" source references.
	reLineNum = regexp.MustCompile(`\bline \d+\b`)
	// bracketed numeric ids, e.g. [1234].
	reBracketNum = regexp.MustCompile(`\[\d+\]`)
	// single-quoted one-character tokens (parser errors: 'x', 'y').
	reSingleChar = regexp.MustCompile(`'[^']'`)
	// trailing file:line[:col] references.
	reLineCol = regexp.MustCompile(`:\d+(?::\d+)?`)
	// whitespace runs, collapsed so spacing differences don't split families.
	reSpace = regexp.MustCompile(`\s+`)
)

// normalizeErrorShape reduces an error message to a volatile-free shape. It is
// pure: identical input always yields identical output.
func normalizeErrorShape(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = reTimestamp.ReplaceAllString(s, "<time>")
	s = reClock.ReplaceAllString(s, "<time>")
	s = reDuration.ReplaceAllString(s, "<dur>")
	s = reHexAddr.ReplaceAllString(s, "<addr>")
	s = reQuotedPath.ReplaceAllString(s, "<path>")
	s = reTempPath.ReplaceAllString(s, "<path>")
	s = reAbsPath.ReplaceAllString(s, "${1}<path>")
	s = rePID.ReplaceAllString(s, "pid <n>")
	s = reLineNum.ReplaceAllString(s, "line <n>")
	s = reBracketNum.ReplaceAllString(s, "[<n>]")
	s = reSingleChar.ReplaceAllString(s, "<char>")
	s = reLineCol.ReplaceAllString(s, ":<n>")
	s = reSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// errorFamilyKey identifies a failure by (tool name, normalized error shape).
// Two failing results collide on the same key only when they share a tool and
// their messages differ solely in scrubbed detail.
func errorFamilyKey(toolName, content string) string {
	norm := normalizeErrorShape(content)
	sum := sha256.Sum256([]byte(toolName + "\x00" + norm))
	return toolName + ":" + hex.EncodeToString(sum[:8])
}
