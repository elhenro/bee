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

import "regexp"

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
	// list bullets, headings, and bold/italic/code emphasis.
	lead = "[\\s>*_`#+-]*"
	// emphasis allowed between the keyword and its colon: `**DONE**:`.
	gap = "[\\s*_`]*"
)

var (
	doneRe    = regexp.MustCompile(`(?im)^` + lead + `DONE` + gap + `:`)
	blockedRe = regexp.MustCompile(`(?im)^` + lead + `BLOCKED` + gap + `:`)
	needsRe   = regexp.MustCompile(`(?im)^` + lead + `NEEDS-INPUT` + gap + `:`)
)

// Classify returns the first sentinel kind that appears anchored to the
// start of any line in s, or KindNone when no sentinel is present.
//
// DONE wins over BLOCKED/NEEDS-INPUT if multiple match — agents that complete
// the objective shouldn't be misclassified as blocked just because they
// quoted prior failure text.
func Classify(s string) Kind {
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

// IsDone is a thin shortcut for the common DONE check.
func IsDone(s string) bool { return doneRe.MatchString(s) }

// IsBlocked is a thin shortcut for the common BLOCKED check.
func IsBlocked(s string) bool { return blockedRe.MatchString(s) }

// IsNeedsInput is a thin shortcut for the NEEDS-INPUT check.
func IsNeedsInput(s string) bool { return needsRe.MatchString(s) }
