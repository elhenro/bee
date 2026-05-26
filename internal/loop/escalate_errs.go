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

// ErrEscalate is the typed sentinel for the `escalate` tool. callers match
// via errors.Is to detect "the model chose to stop and ask the user".
var ErrEscalate = errors.New("loop: model escalated to user")

// EscalateError wraps the escalate tool's payload so callers (TUI, headless
// run) can surface the model's reason + suggested-next-action in the exit
// message instead of just a generic sentinel.
type EscalateError struct {
	Reason     string
	NextAction string
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
