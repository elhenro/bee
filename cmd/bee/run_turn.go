package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"github.com/elhenro/bee/internal/loop"
)

// runWithResume drives one headless turn with bounded auto-resume: a turn that
// stops on a recoverable condition (timeout, dropped stream, transient wedge,
// max-iter) is re-triggered with a continuation instead of giving up, capped
// at the watchdog's resume count so a hopeless spiral can't burn forever.
// Watchdog disabled => exactly one attempt.
func runWithResume(parent context.Context, eng *loop.Engine, msg string) (loop.RunResult, error) {
	cfg := eng.Cfg
	for attempt := 0; ; attempt++ {
		runCtx, cancel := turnContext(parent, cfg.Watchdog)
		res, err := eng.Run(runCtx, msg)
		cancel()

		if !cfg.Watchdog.Enabled {
			return res, err
		}
		d := loop.ClassifyResume(err, res)
		if !d.Resume || attempt >= cfg.Watchdog.Resumes() {
			return res, err
		}
		fmt.Fprintf(os.Stderr, "bee: turn stopped (%s), auto-resuming %d/%d…\n",
			d.Reason, attempt+1, cfg.Watchdog.Resumes())
		// carry context into the next attempt, minus any tool_use left dangling
		// by the aborted dispatch (replaying those is a wire error). for
		// provider-reject the broken tool_use is usually preceded by a long
		// text preamble that drove the model into the wedge — drop the whole
		// trailing assistant message so the next attempt doesn't re-derive
		// the same broken approach. other resume reasons keep the text
		// (it's useful context for a fresh turn).
		if d.Reason == "provider-reject" {
			eng.InitialMessages = loop.StripBrokenToolCall(res.Messages)
		} else {
			eng.InitialMessages = loop.StripDanglingToolUse(res.Messages)
		}
		msg = d.Continuation
	}
}

// wedgeErrors exit with code 7 so wrappers (zzz, swarm, CI) can tell "the
// model wedged, switch model or rephrase" apart from a generic failure.
var wedgeErrors = []error{
	loop.ErrTwoStrike,         // same failing call repeated
	loop.ErrPerToolFailureCap, // N failures on one tool name regardless of args
	loop.ErrFormatStrike,      // malformed envelopes the parser can't recognize
	loop.ErrRepeatStream,      // same text emitted until cut
	loop.ErrEmptyCompletion,   // whitespace-only turns (often a broken thinking template)
	loop.ErrMaxIterations,     // resumes exhausted, run needs more rounds than the cap
}

// exitCodeForErr maps a failed headless run to its process exit code: 7 for a
// wedge, 8 when the model escalated (asked for help rather than crashed), else 1.
func exitCodeForErr(err error) int {
	switch {
	case slices.ContainsFunc(wedgeErrors, func(w error) bool { return errors.Is(err, w) }):
		return 7
	case errors.Is(err, loop.ErrEscalate):
		return 8
	}
	return 1
}
