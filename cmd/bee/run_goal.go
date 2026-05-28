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
			fmt.Fprintf(os.Stderr, "\nbee run: %v\n", err)
			fmt.Fprintf(os.Stderr, "bee back %s\n", sessID)
			os.Exit(1)
		}
		st.Tick()
		if exceeded, reason := st.CapsExceeded(cumTokens(eng)); exceeded {
			fmt.Fprintln(os.Stderr, "goal: stopped ("+reason+")")
			break
		}
		evalCtx, cancel := context.WithTimeout(ctx, goalEvalTimeout)
		v, _ := goal.Evaluate(evalCtx, eng.Provider, config.FastModelOf(cfg), cond, res.Messages)
		cancel()
		if v.Met {
			fmt.Fprintln(os.Stderr, "✓ goal achieved: "+v.Reason)
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
