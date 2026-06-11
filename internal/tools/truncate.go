package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxOutputTokens is the default cap for a tool's text result, in token estimates
// (using the chars/4 heuristic that the rest of bee uses).
const MaxOutputTokens = 50_000

// MaxWebfetchTokens is the aggressive cap for fetch-style tools that return arbitrary remote content.
const MaxWebfetchTokens = 10_000

// truncatableLimits is the per-tool cap table in tokens (chars/4 heuristic).
// Tools absent from this map fall back to MaxOutputTokens via limitFor.
// Caps trimmed by tool shape: chatty stdout (bash) and noisy file dumps
// (read) get tighter ceilings than search/grep which the model often relies
// on for breadth.
var truncatableLimits = map[string]int{
	"bash":          20_000,
	"read":          15_000,
	"search":        30_000,
	"grep":          30_000,
	"glob":          MaxOutputTokens,
	"ls":            5_000,
	"edit":          MaxOutputTokens,
	"apply_patch":   MaxOutputTokens,
	"hashline_edit": MaxOutputTokens,
	"write":         MaxOutputTokens,
	"webfetch":      MaxWebfetchTokens,
	"skill_mcp":     MaxOutputTokens,
}

// limitFor returns the per-tool cap. Unknown tools get MaxOutputTokens.
func limitFor(toolName string) int {
	if n, ok := truncatableLimits[toolName]; ok {
		return n
	}
	return MaxOutputTokens
}

// truncateMode controls which portion of the output is preserved when a
// tool result exceeds its cap.
type truncateMode int

const (
	truncateModeHead     truncateMode = iota // keep prefix only (read: file start)
	truncateModeHeadTail                     // keep both ends with a marker between (default for execution tools: errors live at tail, context at head)
)

// truncateModes maps tool names to their preferred truncation shape. Tools
// absent from the map fall back to truncateModeHead — safest for file
// readers and search-style breadth tools.
//
// bash/shell-style outputs get head-tail: model loses the middle of a long
// test log but keeps the panic/diff at the end, which is where the
// actionable signal sits.
var truncateModes = map[string]truncateMode{
	"bash": truncateModeHeadTail,
}

func modeFor(toolName string) truncateMode {
	if m, ok := truncateModes[toolName]; ok {
		return m
	}
	return truncateModeHead
}

// Truncate caps content to limitFor(toolName) tokens (chars/4 heuristic). When
// the content exceeds the cap, the head is kept and a trailer is appended.
// Returns the (possibly modified) content and a bool indicating whether
// truncation occurred.
func Truncate(toolName, content string) (string, bool) {
	return TruncateWithLimit(toolName, content, 0)
}

// TruncateForTool applies the per-tool default cap and returns only the
// (possibly modified) content. Convenience wrapper for callers that don't
// care about the truncated bool.
func TruncateForTool(toolName, content string) string {
	out, _ := TruncateWithLimit(toolName, content, 0)
	return out
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
//
// Per-tool truncateMode controls shape: head-only by default; head-tail for
// bash so the panic/error at the end of a long test log survives the cut.
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
	// head-tail mode for execution-style outputs: keep ~2/3 head + ~1/3
	// tail so the actionable error block at the end of a test/build log
	// reaches the model alongside the initial context.
	if modeFor(toolName) == truncateModeHeadTail {
		out, _ := truncateHeadTailWithSpill(toolName, content, limitTokens, maxChars/3, spillDir)
		return out, true
	}
	head := content[:maxChars]
	// avoid splitting mid-line: trim back to the last newline in head. when the
	// output is one giant line (minified json, a long diff) there is no newline
	// to snap to — back off the raw byte cut to a rune boundary so we never emit
	// a partial UTF-8 sequence that breaks downstream encoding or model parse.
	if idx := strings.LastIndexByte(head, '\n'); idx > 0 {
		head = head[:idx]
	} else {
		head = trimPartialRune(head)
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

// trimPartialRune drops a trailing incomplete UTF-8 sequence left by a raw byte
// cut. Backs off at most utf8.UTFMax bytes — a complete rune (including a real
// U+FFFD, which decodes with size>1) stops the trim immediately.
func trimPartialRune(s string) string {
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size > 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// advanceToRuneStart moves i forward to the next UTF-8 rune boundary so a tail
// slice taken at a raw byte offset doesn't begin mid-sequence.
func advanceToRuneStart(s string, i int) int {
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return i
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

// truncateHeadTailWithSpill is the head-tail variant of TruncateWithLimitSpill.
// Identical spill behavior (full body persisted, trailer points to file when
// available) but the message keeps both ends of the original.
func truncateHeadTailWithSpill(toolName, content string, limitTokens, tailChars int, spillDir string) (string, bool) {
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
	if tailChars < 0 {
		tailChars = 4096
	}
	maxTailChars := total - maxChars
	if tailChars > maxTailChars {
		tailChars = maxTailChars
	}
	// floor the head at half the budget so a large tail can never starve the
	// initial context (the command + start of the log the model needs to read).
	if tailChars > maxChars/2 {
		tailChars = maxChars / 2
	}
	head := content[:maxChars-tailChars]
	if idx := strings.LastIndexByte(head, '\n'); idx > 0 {
		head = head[:idx]
	} else {
		head = trimPartialRune(head)
	}
	tailStart := total - tailChars
	if nl := strings.Index(content[tailStart:], "\n"); nl >= 0 {
		tailStart += nl
	} else {
		tailStart = advanceToRuneStart(content, tailStart)
	}
	tail := content[tailStart:]
	skipped := total - len(head) - len(tail)
	spillPath := writeSpill(spillDir, toolName, content)
	var trailer string
	if spillPath != "" {
		trailer = fmt.Sprintf("\n...\n(truncated middle: kept first %d and last %d of %d; skipped %d)\n\n[full output saved to %s]\n...",
			len(head), len(tail), total, skipped, spillPath)
	} else {
		trailer = fmt.Sprintf("\n...\n(truncated middle: kept first %d and last %d of %d; skipped %d)\n...",
			len(head), len(tail), total, skipped)
	}
	return head + trailer + tail, true
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
	// avoid splitting mid-line: trim back to the last newline in head; with no
	// newline, back off to a rune boundary so we never emit a partial UTF-8 seq.
	if idx := strings.LastIndexByte(head, '\n'); idx > 0 {
		head = head[:idx]
	} else {
		head = trimPartialRune(head)
	}
	tailStart := strings.Index(content[len(content)-tailChars:], "\n")
	if tailStart >= 0 {
		tailStart += len(content) - tailChars
	}
	if tailStart < 0 {
		// tail portion starts from beginning — no tail to show (pathological)
		return trimPartialRune(content[:maxChars]), true
	}
	tail := content[tailStart:]
	skipped := total - len(head) - len(tail)
	trailer := fmt.Sprintf("\n...\n(skipped %d chars; kept first %d chars of %d and last %d; use limit/offset on read or grep to see middle)\n...", skipped, len(head), total, len(tail))
	return head + trailer + tail, true
}
