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
// handling for the first failure. Returns 1 on that failure (the caller exits
// after flushing its own state), else 0.
func runGoalHeadless(ctx context.Context, eng *loop.Engine, cfg config.Config, sessID, cond string) int {
	var st goal.State
	st.Set(cond, 0, time.Now())
	msg := cond
	wedgeStreak := 0
	stalledStreak := 0
	prevReason := ""
	for {
		if ctx.Err() != nil {
			fmt.Fprintln(os.Stderr, "goal: cancelled")
			return 0
		}
		res, err := eng.Run(ctx, msg)
		carryHistory(eng, res, err)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(os.Stderr, "goal: cancelled")
				return 0
			}
			// wedged turn (repeated failing call, per-tool cap, malformed envelope,
			// output loop): not fatal, interactive sessions survive these too.
			// spend the turn, reframe, retry — but give up after several in a row
			// so a hopeless spiral can't burn the cap. escalate is excluded: that
			// is the model deliberately stopping to ask the user.
			if loop.IsWedge(err) {
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
			return 1
		}
		wedgeStreak = 0
		st.Tick()
		if exceeded, reason := st.CapsExceeded(cumTokens(eng)); exceeded {
			fmt.Fprintln(os.Stderr, goal.StoppedPrefix+reason+")")
			break
		}
		evalCtx, cancel := context.WithTimeout(ctx, goal.EvalTimeout)
		v, _ := goal.Evaluate(evalCtx, eng.Provider, config.FastModelOf(cfg), cond, res.Messages)
		cancel()
		if v.Met {
			fmt.Fprintln(os.Stderr, goal.AchievedPrefix+v.Reason)
			break
		}
		if st.Replies.Observe(res.FinalText) {
			fmt.Fprintln(os.Stderr, goal.StoppedPrefix+goal.StalledReason(st.Replies.Count())+")")
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
	return 0
}

// carryHistory seeds the next turn with this turn's transcript: Run is
// stateless, so without it every continuation starts blind (and the goal judge
// sees only the latest turn). a tool_use left dangling by a failed turn is dropped.
func carryHistory(eng *loop.Engine, res loop.RunResult, err error) {
	if len(res.Messages) == 0 {
		return
	}
	if errors.Is(err, loop.ErrProviderReject) {
		eng.InitialMessages = loop.StripBrokenToolCall(res.Messages)
		return
	}
	eng.InitialMessages = loop.StripDanglingToolUse(res.Messages)
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
