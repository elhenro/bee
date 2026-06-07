package loop

import (
	"errors"
	"fmt"

	"github.com/elhenro/bee/internal/types"
)

// loop-level sentinel errors. callers can match via errors.Is / errors.As.

// ErrTwoStrike indicates the same tool call (name + args) errored twice in
// a row. caller should stop looping and surface the cause to the user.
var ErrTwoStrike = errors.New("loop: tool call failed twice in a row")

// ErrPerToolFailureCap indicates a single tool name has errored K times in
// a row regardless of args. signals the model is wedged on a specific tool.
var ErrPerToolFailureCap = errors.New("loop: tool failed beyond per-tool cap")

// ErrFormatStrike indicates the model emitted text that LOOKED like a tool
// call (XML or JSON envelope) N times in a row without the parser recognizing
// any of them. signals the model is wedged on a malformed envelope shape that
// no amount of nudging will fix — bail and let the user / wrapper switch
// model or prompt.
var ErrFormatStrike = errors.New("loop: format strike — model wedged on malformed envelope")

// ErrEscalate is the typed sentinel for the `escalate` tool. callers match
// via errors.Is to detect "the model chose to stop and ask the user".
var ErrEscalate = errors.New("loop: model escalated to user")

// EscalateError wraps the escalate tool's payload so callers (TUI, headless
// run) can surface the model's reason + suggested-next-action in the exit
// message instead of just a generic sentinel.
type EscalateError struct {
	Reason     string
	NextAction string
	Options    []string
}

func (e *EscalateError) Error() string {
	if e.NextAction == "" {
		return fmt.Sprintf("%s: %s", ErrEscalate.Error(), e.Reason)
	}
	return fmt.Sprintf("%s: %s — next: %s", ErrEscalate.Error(), e.Reason, e.NextAction)
}

func (e *EscalateError) Is(target error) bool { return target == ErrEscalate }
func (e *EscalateError) Unwrap() error        { return ErrEscalate }

// TwoStrikeError wraps the offending ToolUse so callers (TUI, headless
// `bee run`) can surface tool name + args in the exit message.
type TwoStrikeError struct {
	Use   types.ToolUse
	Class string // tool-error class tag (toolErrNotFound, toolErrTimeout, etc.)
}

func (e *TwoStrikeError) Error() string {
	return fmt.Sprintf("%s: tool=%s class=%s", ErrTwoStrike.Error(), e.Use.Name, e.Class)
}

// Is lets errors.Is(err, ErrTwoStrike) match wrapped variants.
func (e *TwoStrikeError) Is(target error) bool { return target == ErrTwoStrike }

// Unwrap surfaces the sentinel for errors.Is chains.
func (e *TwoStrikeError) Unwrap() error { return ErrTwoStrike }

// PerToolFailureError wraps a per-tool-cap bail with the offending tool name
// and the streak length so callers can surface "bash failed 8x in a row".
type PerToolFailureError struct {
	Use    types.ToolUse
	Tool   string
	Streak int
	Class  string
}

func (e *PerToolFailureError) Error() string {
	return fmt.Sprintf("%s: tool=%s streak=%d class=%s", ErrPerToolFailureCap.Error(), e.Tool, e.Streak, e.Class)
}

func (e *PerToolFailureError) Is(target error) bool { return target == ErrPerToolFailureCap }
func (e *PerToolFailureError) Unwrap() error        { return ErrPerToolFailureCap }

// FormatStrikeError wraps a format-strike bail with the streak length so
// callers can render "model emitted malformed envelopes 3x — switch model".
type FormatStrikeError struct {
	Streak int
}

func (e *FormatStrikeError) Error() string {
	return fmt.Sprintf("%s: streak=%d", ErrFormatStrike.Error(), e.Streak)
}

func (e *FormatStrikeError) Is(target error) bool { return target == ErrFormatStrike }
func (e *FormatStrikeError) Unwrap() error        { return ErrFormatStrike }

// ErrRepeatStream indicates the model's stream was cut for degenerate
// repetition (the same phrase looped) N turns in a row — it's wedged in a token
// loop that nudging won't fix. bail and let the user / wrapper switch model.
var ErrRepeatStream = errors.New("loop: stream stuck repeating itself")

// RepeatStreamError wraps a repetition-loop bail with the streak length so
// callers can render "model looped its output 3x — switch model".
type RepeatStreamError struct {
	Streak int
}

func (e *RepeatStreamError) Error() string {
	return fmt.Sprintf("%s: streak=%d", ErrRepeatStream.Error(), e.Streak)
}

func (e *RepeatStreamError) Is(target error) bool { return target == ErrRepeatStream }
func (e *RepeatStreamError) Unwrap() error        { return ErrRepeatStream }

// ErrTruncatedStream indicates the provider stream dropped mid-output (a
// transient network error AFTER content already streamed) N turns in a row.
// The first such drops are recovered — bee keeps the partial turn and nudges
// the model to continue — but a persistent drop means the connection is dead,
// so bail rather than reconnect forever.
var ErrTruncatedStream = errors.New("loop: stream dropped mid-output repeatedly")

// TruncatedStreamError wraps a mid-stream-drop bail with the streak length.
type TruncatedStreamError struct {
	Streak int
}

func (e *TruncatedStreamError) Error() string {
	return fmt.Sprintf("%s: streak=%d", ErrTruncatedStream.Error(), e.Streak)
}

func (e *TruncatedStreamError) Is(target error) bool { return target == ErrTruncatedStream }
func (e *TruncatedStreamError) Unwrap() error        { return ErrTruncatedStream }

// ErrMaxIterations indicates the loop hit its tool-use round cap without the
// model signalling done. Not a wedge — the model was making progress, it just
// ran out of budget — so the watchdog can resume it with a plain "continue".
var ErrMaxIterations = errors.New("loop: hit max iterations")

// MaxIterationsError carries the cap so callers can surface it and so
// errors.Is(err, ErrMaxIterations) matches. Error() keeps the original
// guidance text (type 'continue' / /iterations) callers and tests rely on.
type MaxIterationsError struct {
	Limit int
}

func (e *MaxIterationsError) Error() string {
	return fmt.Sprintf("loop: hit max iterations (%d) — type 'continue' to resume, "+
		"raise it with /iterations <n>, or remove the limit with /iterations 0 "+
		"(or set max_iterations = 0 in config)", e.Limit)
}

func (e *MaxIterationsError) Is(target error) bool { return target == ErrMaxIterations }
func (e *MaxIterationsError) Unwrap() error        { return ErrMaxIterations }

// ErrEmptyCompletion indicates the provider returned whitespace-only output —
// no text, no reasoning, no tool_use — N turns in a row. A nudge couldn't
// shake it loose, so the model (or its inference template) is producing dead
// turns; bail and let the user switch model instead of spinning the iter
// budget. Commonly a thinking-suppression switch the chat template mishandles.
var ErrEmptyCompletion = errors.New("loop: model returned empty output repeatedly")

// EmptyCompletionError wraps an empty-completion bail with the streak length.
type EmptyCompletionError struct {
	Streak int
}

func (e *EmptyCompletionError) Error() string {
	return fmt.Sprintf("%s: streak=%d", ErrEmptyCompletion.Error(), e.Streak)
}

func (e *EmptyCompletionError) Is(target error) bool { return target == ErrEmptyCompletion }
func (e *EmptyCompletionError) Unwrap() error        { return ErrEmptyCompletion }
