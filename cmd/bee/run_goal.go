package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/goal"
	"github.com/elhenro/bee/internal/loop"
)

// goalEvalTimeout caps each side-call so a stuck fast model can't wedge the
// headless loop.
const goalEvalTimeout = 30 * time.Second

// maxConsecutiveWedges bounds recovery from wedged turns: after this many turns
// in a row bail with no progress, the loop gives up rather than burning the
// whole turn cap on a hopeless spiral. Reset by any turn that finishes cleanly.
const maxConsecutiveWedges = 3

// maxStalledContinuations bounds the not-met retry loop: a clean turn that the
// judge rules not-met with the SAME reason as last time is no progress. After
// this many identical not-met reasons in a row, stop instead of spinning to the
// turn cap. A changed reason counts as progress and resets the streak.
const maxStalledContinuations = 3

// stalledStep advances the no-progress counter. identical not-met reasons
// increment it; a changed reason resets to 1. distinct from the wedge streak.
func stalledStep(streak int, prev, reason string) (int, string) {
	if reason == prev {
		return streak + 1, prev
	}
	return 1, reason
}

// isWedgedTurn reports whether err is a "this turn got stuck" signal (repeated
// failing call, per-tool cap, malformed envelope, output loop) rather than a
// fatal error. Interactive sessions survive these — the headless goal loop
// should too: spend the turn, reframe, retry. Escalate is excluded — that is
// the model deliberately stopping to ask the user.
func isWedgedTurn(err error) bool {
	return errors.Is(err, loop.ErrTwoStrike) ||
		errors.Is(err, loop.ErrPerToolFailureCap) ||
		errors.Is(err, loop.ErrFormatStrike) ||
		errors.Is(err, loop.ErrRepeatStream)
}

// parseGoalMessage detects a "/goal …" headless request and returns the
// trimmed condition. show/clear/aliases and the bare command return ("", true)
// so the caller can print a hint and exit. Non-goal messages return ("", false).
func parseGoalMessage(userMsg string) (cond string, isGoal bool) {
	t := strings.TrimSpace(userMsg)
	if t != "/goal" && !strings.HasPrefix(t, "/goal ") && !strings.HasPrefix(t, "/goal\t") {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(t, "/goal"))
	if rest == "" {
		return "", true
	}
	if goal.IsClearWord(rest) || rest == "show" {
		return "", true
	}
	return rest, true
}

// runGoalHeadless loops eng.Run until a fast model judges cond met, caps are
// exceeded, an error occurs, or the user cancels. Mirrors the single-run error
// handling for the first failure.
func runGoalHeadless(ctx context.Context, eng *loop.Engine, cfg config.Config, sessID, cond string) {
	var st goal.State
	st.Set(cond, 0, time.Now())
	msg := cond
	wedgeStreak := 0
	stalledStreak := 0
	prevReason := ""
	for {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "goal: cancelled")
			return
		}
		res, err := eng.Run(ctx, msg)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "goal: cancelled")
				return
			}
			// wedged turn: not fatal. spend the turn, reframe, retry — but give
			// up after several in a row so a hopeless spiral can't burn the cap.
			if isWedgedTurn(err) {
				st.Tick()
				wedgeStreak++
				if wedgeStreak >= maxConsecutiveWedges {
					fmt.Fprintf(os.Stderr, goal.StoppedPrefix+"model wedged %d turns in a row: %v)\n", wedgeStreak, err)
					break
				}
				if exceeded, reason := st.CapsExceeded(cumTokens(eng)); exceeded {
					fmt.Fprintln(os.Stderr, goal.StoppedPrefix+reason+")")
					break
				}
				fmt.Fprintf(os.Stderr, "goal: turn wedged (%v), recovering…\n", err)
				msg = goal.RecoverContinuation(cond, err.Error())
				continue
			}
			fmt.Fprintf(os.Stderr, "\nbee run: %v\n", err)
			fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
			os.Exit(1)
		}
		wedgeStreak = 0
		st.Tick()
		if exceeded, reason := st.CapsExceeded(cumTokens(eng)); exceeded {
			fmt.Fprintln(os.Stderr, goal.StoppedPrefix+reason+")")
			break
		}
		evalCtx, cancel := context.WithTimeout(ctx, goalEvalTimeout)
		v, _ := goal.Evaluate(evalCtx, eng.Provider, config.FastModelOf(cfg), cond, res.Messages)
		cancel()
		if v.Met {
			fmt.Fprintln(os.Stderr, goal.AchievedPrefix+v.Reason)
			break
		}
		stalledStreak, prevReason = stalledStep(stalledStreak, prevReason, v.Reason)
		if stalledStreak >= maxStalledContinuations {
			fmt.Fprintf(os.Stderr, goal.StoppedPrefix+"no progress after %d continuations: %s)\n", stalledStreak, v.Reason)
			break
		}
		fmt.Fprintln(os.Stderr, "goal not met ("+v.Reason+"), continuing…")
		msg = goal.Continuation(cond, v.Reason)
	}
	fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
}

// cumTokens reports cumulative session tokens from the engine's cost tracker,
// or 0 when none is wired (caps then key off turn count only).
func cumTokens(eng *loop.Engine) int {
	if eng == nil || eng.Costs == nil {
		return 0
	}
	t := eng.Costs.Total()
	return t.Input + t.Output
}
