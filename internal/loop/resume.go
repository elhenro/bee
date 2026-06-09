package loop

import (
	"context"
	"errors"

	"github.com/elhenro/bee/internal/types"
)

// resume continuation messages. generic (no goal condition) so the same text
// serves the headless plain-run driver and the TUI watchdog. mirrors the
// spirit of goal.RecoverContinuation.
const (
	resumeTimeoutMsg = "[resume] Your previous turn timed out before finishing. " +
		"Continue from where you left off — call a tool to make progress or give a short final answer."
	resumeStreamMsg = "[resume] The connection dropped before you finished. " +
		"Continue from where you left off."
	resumeWedgeMsg = "[resume] Your previous turn got stuck repeating a failing or malformed call. " +
		"Do not repeat it: re-read the last error, fill every required argument, or switch approach. " +
		"If you genuinely cannot proceed, call the escalate tool instead of retrying. " +
		"When done, state clearly what you completed."
	resumeContinueMsg = "[resume] Continue from where you left off."
)

// IsWedge reports the "this turn got stuck, a reframed retry is sane" family.
// This is the exact set cmd/bee/run_goal.go inlines as isWedgedTurn — extracted
// so the goal loop and the watchdog share one definition. Deliberately excludes
// ErrTruncatedStream, context.DeadlineExceeded, and ErrMaxIterations: those are
// handled as their own resume reasons, not as wedges.
func IsWedge(err error) bool {
	return errors.Is(err, ErrTwoStrike) ||
		errors.Is(err, ErrPerToolFailureCap) ||
		errors.Is(err, ErrFormatStrike) ||
		errors.Is(err, ErrRepeatStream) ||
		errors.Is(err, ErrEmptyCompletion)
}

// ResumeDecision is the policy output. Stateless — the caller owns the counter
// and the cap.
type ResumeDecision struct {
	Resume       bool
	Continuation string // synthetic user message to re-trigger with
	Reason       string // short label for the warn line / stderr
}

// ClassifyResume maps a finished Run (err + result) to a resume decision. The
// non-resumable cases are checked first so a clean finish, a user abort, or a
// deliberate escalate always wins over any resumable signal.
//
// Resumable: a hang surfaced as context.DeadlineExceeded, a persistently
// dropped stream, a model wedge (after the loop's own in-turn nudges gave up),
// and hitting the iteration cap. Non-resumable: clean finish, user cancel,
// escalate, hard budget cap, and any unrecognised/fatal error (auth, provider
// down, config) — those should fail fast, not spin the resume budget.
func ClassifyResume(err error, res RunResult) ResumeDecision {
	switch {
	case err == nil,
		errors.Is(err, context.Canceled),
		errors.Is(err, ErrEscalate):
		return ResumeDecision{}
	case errors.Is(err, context.DeadlineExceeded):
		return ResumeDecision{Resume: true, Continuation: resumeTimeoutMsg, Reason: "timeout"}
	case errors.Is(err, ErrTruncatedStream):
		return ResumeDecision{Resume: true, Continuation: resumeStreamMsg, Reason: "stream-drop"}
	case IsWedge(err):
		return ResumeDecision{Resume: true, Continuation: resumeWedgeMsg, Reason: "wedged"}
	case errors.Is(err, ErrMaxIterations):
		return ResumeDecision{Resume: true, Continuation: resumeContinueMsg, Reason: "max-iter"}
	default:
		return ResumeDecision{}
	}
}

// ShouldResume is the thin (bool, continuation) wrapper over ClassifyResume.
func ShouldResume(err error, res RunResult) (bool, string) {
	d := ClassifyResume(err, res)
	return d.Resume, d.Continuation
}

// StripDanglingToolUse sanitizes a message history before it is carried into
// a resumed run. A turn that died mid-dispatch can end on an assistant
// message whose tool_use blocks never got a tool_result; replaying that
// history is a wire error on strict providers and bait for hallucinated
// results on lenient ones. Drops the dangling tool_use blocks, and the whole
// trailing message when nothing else remains in it.
func StripDanglingToolUse(msgs []types.Message) []types.Message {
	if len(msgs) == 0 {
		return msgs
	}
	last := msgs[len(msgs)-1]
	if last.Role != types.RoleAssistant {
		return msgs
	}
	kept := make([]types.ContentBlock, 0, len(last.Content))
	for _, b := range last.Content {
		if b.Type == types.BlockToolUse {
			continue
		}
		kept = append(kept, b)
	}
	if len(kept) == len(last.Content) {
		return msgs
	}
	if len(kept) == 0 {
		return msgs[:len(msgs)-1]
	}
	out := make([]types.Message, len(msgs))
	copy(out, msgs)
	out[len(out)-1].Content = kept
	return out
}
