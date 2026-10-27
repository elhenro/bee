// Package sentinel centralizes the loop-control markers an unattended bee
// agent uses to signal turn outcomes back to the orchestrator.
//
// Both `bee zzz` (single-objective overnight loop) and `bee agents` (parallel
// worktree agents) speak the same protocol — keeping the regex set in one
// place avoids drift, makes the contract self-documenting, and gives both
// orchestrators identical detection semantics. The status enums those
// orchestrators write to disk remain distinct: zzz tracks RUN lifecycle
// (running/completed/failed/aborted); bgreg.Status tracks AGENT-turn state
// (active/awaiting/done/failed/idle). Different abstractions, intentionally
// not collapsed.
package sentinel

import (
	"regexp"
	"strings"
)

// Kind classifies the sentinel found in an agent's final text.
type Kind int

const (
	KindNone Kind = iota
	KindDone
	KindBlocked
	KindNeedsInput
)

// String renders the sentinel kind in the same casing the agent emits.
func (k Kind) String() string {
	switch k {
	case KindDone:
		return "DONE"
	case KindBlocked:
		return "BLOCKED"
	case KindNeedsInput:
		return "NEEDS-INPUT"
	}
	return ""
}

// Weak / quantized local models rarely emit a bare `DONE:` — they wrap it in
// markdown the prompt never asked for: `**DONE:**`, `- DONE:`, `> DONE:`,
// `## DONE:`. A strict `^\s*` anchor misses all of those, so an objective that
// is actually finished keeps looping to the iteration cap. These patterns stay
// line-anchored (mid-line `DONE` is still ignored) but skip leading list /
// quote / heading / emphasis markers and emphasis hugging the keyword.
const (
	// leading decoration allowed before the keyword: whitespace, blockquote,
	// list bullets, headings, and bold/italic/code emphasis. A -/+ bullet
	// needs its space, so a quoted diff line (`+DONE: x`) is not a marker.
	lead = "(?:[\\s>*_`#]|[-+]\\s)*"
	// emphasis allowed between the keyword and its colon: `**DONE**:`.
	gap = "[\\s*_`]*"
)

var (
	doneRe    = regexp.MustCompile(`(?m)^` + lead + `DONE` + gap + `:`)
	blockedRe = regexp.MustCompile(`(?m)^` + lead + `BLOCKED` + gap + `:`)
	needsRe   = regexp.MustCompile(`(?m)^` + lead + `NEEDS-INPUT` + gap + `:`)
)

// Markers are case-sensitive (the protocol asks for DONE:, and prose like
// "Done: added x" is not a signal) and are ignored inside fenced code blocks,
// where an agent quotes the protocol or tool output.
//
// Classify returns the first sentinel kind that appears anchored to the
// start of any line in s, or KindNone when no sentinel is present.
//
// DONE wins over BLOCKED/NEEDS-INPUT if multiple match — agents that complete
// the objective shouldn't be misclassified as blocked just because they
// quoted prior failure text.
func Classify(s string) Kind {
	s = stripFences(s)
	switch {
	case doneRe.MatchString(s):
		return KindDone
	case blockedRe.MatchString(s):
		return KindBlocked
	case needsRe.MatchString(s):
		return KindNeedsInput
	}
	return KindNone
}

// stripFences drops fenced code blocks. An unterminated fence swallows the
// rest of the text, as markdown renderers do.
func stripFences(s string) string {
	if !strings.Contains(s, "```") && !strings.Contains(s, "~~~") {
		return s
	}
	var out []string
	fence := ""
	for line := range strings.SplitSeq(s, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
		case strings.HasPrefix(t, "```"):
			fence = "```"
		case strings.HasPrefix(t, "~~~"):
			fence = "~~~"
		default:
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
