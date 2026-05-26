package loop

import (
	"fmt"

	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/types"
)

// repeat-detect thresholds. constants because tuning these requires telemetry
// runs, not config knobs the user fiddles with.
const (
	repeatNudgeAt      = 3 // same call (success or fail) fired N times → nudge
	perToolFailNudgeAt = 3 // same tool name failed N times in a row → nudge
)

// observeRepeats walks the just-finished tool dispatch, feeds each result
// into the per-Run tracker, and:
//   - appends warning prefixes for repeat / per-tool-failure thresholds
//   - emits one `tool_repeat` NDJSON event per crossing (for --json consumers)
//   - returns a non-nil error when two-strike fires so caller bails the loop.
//
// blocks already carries the tool-result content; we only prepend warnings.
func observeRepeats(e *Engine, uses []types.ToolUse, results []types.ToolResult, blocks []types.ContentBlock) ([]types.ContentBlock, error) {
	if e.repeats == nil {
		e.repeats = newRepeatTracker()
	}
	// index results by UseID so we can pair use ↔ isError reliably even when
	// safeParallelTools shuffles in-flight order.
	byUseID := make(map[string]bool, len(results))
	for _, r := range results {
		byUseID[r.UseID] = r.IsError
	}
	for _, u := range uses {
		isErr := byUseID[u.ID]
		obs := e.repeats.Observe(u, isErr)

		if obs.IsTwoStrike {
			emitRepeatEvent(e, u, "two_strike", obs)
			return blocks, &TwoStrikeError{Use: u, Class: ""}
		}
		if !e.nudgedRepeat && obs.RepeatCount >= repeatNudgeAt {
			w := fmt.Sprintf("[repeat] same call to %s fired %dx — try a different approach, ask the user, or call escalate.\n\n",
				u.Name, obs.RepeatCount)
			blocks = prependWarningToToolResult(blocks, w)
			e.nudgedRepeat = true
			emitRepeatEvent(e, u, "repeat", obs)
		}
		if !e.nudgedPerToolFail && obs.ConsecutiveSameToolFailures >= perToolFailNudgeAt {
			w := fmt.Sprintf("[tool-fail] %s failed %dx in a row — different tool? different args? ask the user.\n\n",
				u.Name, obs.ConsecutiveSameToolFailures)
			blocks = prependWarningToToolResult(blocks, w)
			e.nudgedPerToolFail = true
			emitRepeatEvent(e, u, "per_tool_fail", obs)
		}
	}
	return blocks, nil
}

// emitRepeatEvent surfaces the signal on the NDJSON channel so headless
// callers (`--json`) can observe without scraping prose.
func emitRepeatEvent(e *Engine, u types.ToolUse, kind string, obs Observation) {
	if e.JSONEmitter == nil {
		return
	}
	e.JSONEmitter.Emit(jsonmode.Event{
		Type:    "tool_repeat",
		Name:    u.Name,
		UseID:   u.ID,
		Content: fmt.Sprintf("kind=%s repeat=%d tool_fail_streak=%d", kind, obs.RepeatCount, obs.ConsecutiveSameToolFailures),
	})
}
