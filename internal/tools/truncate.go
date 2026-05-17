package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxOutputTokens is the default cap for a tool's text result, in token estimates
// (using the chars/4 heuristic that the rest of bee uses).
const MaxOutputTokens = 50_000

// MaxWebfetchTokens is the aggressive cap for fetch-style tools that return arbitrary remote content.
const MaxWebfetchTokens = 10_000

// truncatableLimits is the per-tool cap table. Tools absent from this map fall
// back to MaxOutputTokens via limitFor.
var truncatableLimits = map[string]int{
	"bash":          MaxOutputTokens,
	"search":        MaxOutputTokens,
	"glob":          MaxOutputTokens,
	"ls":            MaxOutputTokens,
	"read":          MaxOutputTokens,
	"edit":          MaxOutputTokens,
	"apply_patch":   MaxOutputTokens,
	"hashline_edit": MaxOutputTokens,
	"write":         MaxOutputTokens,
	// future:
	"webfetch":  MaxWebfetchTokens,
	"skill_mcp": MaxOutputTokens,
}

// limitFor returns the per-tool cap. Unknown tools get MaxOutputTokens.
func limitFor(toolName string) int {
	if n, ok := truncatableLimits[toolName]; ok {
		return n
	}
	return MaxOutputTokens
}

// Truncate caps content to limitFor(toolName) tokens (chars/4 heuristic). When
// the content exceeds the cap, the head is kept and a trailer is appended.
// Returns the (possibly modified) content and a bool indicating whether
// truncation occurred.
func Truncate(toolName, content string) (string, bool) {
	return TruncateWithLimit(toolName, content, 0)
}

// TruncateWithLimit is Truncate with an explicit profile-provided cap in
// tokens (chars/4 heuristic). limitTokens<=0 → fall back to the per-tool
// default. The smaller of (limitTokens, per-tool default) wins so a profile
// override cannot accidentally raise a webfetch-style tight ceiling.
//
// When truncation occurs and a spill directory can be resolved (via
// $BEE_HOME/spill or os.UserHomeDir()+/.bee/spill), the full untruncated
// body is written there and the trailer points the model at it. Spill
// failures degrade gracefully to the original truncate-and-discard.
func TruncateWithLimit(toolName, content string, limitTokens int) (string, bool) {
	return TruncateWithLimitSpill(toolName, content, limitTokens, defaultSpillDir())
}

// TruncateWithLimitSpill is the testable variant: caller supplies the spill
// directory. Empty spillDir disables spillover (truncate-and-discard).
func TruncateWithLimitSpill(toolName, content string, limitTokens int, spillDir string) (string, bool) {
	if content == "" {
		return content, false
	}
	limit := limitFor(toolName)
	if limitTokens > 0 && limitTokens < limit {
		limit = limitTokens
	}
	maxChars := limit * 4
	total := len(content)
	if total <= maxChars {
		return content, false
	}
	head := content[:maxChars]
	// avoid splitting mid-line: trim back to the last newline in head
	if idx := strings.LastIndexByte(head, '\n'); idx > 0 {
		head = head[:idx]
	}
	spillPath := writeSpill(spillDir, toolName, content)
	var trailer string
	if spillPath != "" {
		trailer = fmt.Sprintf("\n...\n(truncated: kept first %d chars of %d)\n\n[output truncated; full body saved to %s — read it if you need the rest]\n",
			len(head), total, spillPath)
	} else {
		trailer = fmt.Sprintf("\n...\n(truncated: kept first %d chars of %d; full output not retained)", len(head), total)
	}
	return head + trailer, true
}

// defaultSpillDir resolves the spill directory from $BEE_HOME (preferred)
// or the user's ~/.bee. Returns empty string when neither is available,
// which disables spillover.
func defaultSpillDir() string {
	if v := os.Getenv("BEE_HOME"); v != "" {
		return filepath.Join(v, "spill")
	}
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return ""
	}
	return filepath.Join(h, ".bee", "spill")
}

// writeSpill persists the full body to spillDir and returns the absolute path.
// Returns "" on any failure (empty dir, mkdir error, write error). Failures
// are logged to stderr but never fatal — caller falls back to plain truncate.
func writeSpill(spillDir, toolName, body string) string {
	if spillDir == "" {
		return ""
	}
	if err := os.MkdirAll(spillDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "bee: spill mkdir %s failed: %v\n", spillDir, err)
		return ""
	}
	ts := time.Now().UTC().Format("20060102T150405")
	short := strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	name := fmt.Sprintf("%s-%s-%s.txt", ts, safeToolName(toolName), short)
	path := filepath.Join(spillDir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "bee: spill write %s failed: %v\n", path, err)
		return ""
	}
	return path
}

// safeToolName strips path separators / spaces so an exotic tool name can't
// break out of spillDir or produce ugly filenames.
func safeToolName(s string) string {
	if s == "" {
		return "tool"
	}
	repl := func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
			return r
		}
		return '_'
	}
	return strings.Map(repl, s)
}

// TruncateHeadTail is like Truncate but preserves both head and tail with a
// spacer in the middle, giving the model context from the end of large outputs.
// tailChars defaults to 4 KB (4096) if < 0. Returns modified content + true.
func TruncateHeadTail(toolName, content string, tailChars int) (string, bool) {
	return TruncateHeadTailWithLimit(toolName, content, 0, tailChars)
}

// TruncateHeadTailWithLimit is TruncateHeadTail with an explicit profile cap.
func TruncateHeadTailWithLimit(toolName, content string, limitTokens int, tailChars int) (string, bool) {
	if content == "" {
		return content, false
	}
	limit := limitFor(toolName)
	if limitTokens > 0 && limitTokens < limit {
		limit = limitTokens
	}
	maxChars := limit * 4
	total := len(content)
	if total <= maxChars {
		return content, false
	}
	// tail defaults to 4 KB if < 0
	if tailChars < 0 {
		tailChars = 4096
	}
	maxTailChars := total - maxChars
	if tailChars > maxTailChars {
		tailChars = maxTailChars
	}
	head := content[:maxChars]
	// avoid splitting mid-line: trim back to the last newline in head
	if idx := strings.LastIndexByte(head, '\n'); idx > 0 {
		head = head[:idx]
	}
	tailStart := strings.Index(content[len(content)-tailChars:], "\n")
	if tailStart >= 0 {
		tailStart += len(content) - tailChars
	}
	if tailStart < 0 {
		// tail portion starts from beginning — no tail to show (pathological)
		return content[:maxChars], true
	}
	tail := content[tailStart:]
	skipped := total - len(head) - len(tail)
	trailer := fmt.Sprintf("\n...\n(skipped %d chars; kept first %d chars of %d and last %d; use limit/offset on read or grep to see middle)\n...", skipped, len(head), total, len(tail))
	return head + trailer + tail, true
}
