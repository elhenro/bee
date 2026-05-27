package loop

import (
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/jsonmode"
	"github.com/elhenro/bee/internal/types"
)

// repeat-detect thresholds. constants because tuning these requires telemetry
// runs, not config knobs the user fiddles with.
const (
	repeatNudgeAt      = 3 // same call (success or fail) fired N times → nudge
	perToolFailNudgeAt = 3 // same tool name failed N times in a row → nudge
	perToolFailBailAt  = 8 // same tool name failed N times in a row → hard bail
	sigFailHardBailAt  = 5 // same (tool,args) failed N times in a row → hard bail
	formatStrikeAt     = 3 // consecutive malformed envelopes → FormatStrikeError
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
	// index results by UseID so we can pair use ↔ content reliably even when
	// safeParallelTools shuffles in-flight order.
	resByID := make(map[string]types.ToolResult, len(results))
	for _, r := range results {
		resByID[r.UseID] = r
	}
	for _, u := range uses {
		r := resByID[u.ID]
		obs := e.repeats.Observe(u, r.IsError)

		// hard bail: same (tool,args) failed N times in a row OR same tool name
		// failed K times in a row. give the model rope (nudges first), but stop
		// when it's clearly wedged. class is parsed from the formatted envelope
		// so the exit message tells the user what kind of failure killed it.
		if obs.ConsecutiveSameSigFailures >= sigFailHardBailAt {
			emitRepeatEvent(e, u, "hard_bail_sig", obs)
			return blocks, &TwoStrikeError{Use: u, Class: parseClassFromResult(r.Content)}
		}
		if obs.ConsecutiveSameToolFailures >= perToolFailBailAt {
			emitRepeatEvent(e, u, "hard_bail_tool", obs)
			return blocks, &PerToolFailureError{
				Use:    u,
				Tool:   u.Name,
				Streak: obs.ConsecutiveSameToolFailures,
				Class:  parseClassFromResult(r.Content),
			}
		}

		// 2-in-a-row soft signal: inject a strong corrective nudge but keep
		// looping. dedupes per Run so a wedged provider isn't spam-nudged.
		if obs.IsTwoStrike && !e.nudgedTwoStrike {
			class := parseClassFromResult(r.Content)
			w := fmt.Sprintf("[two-strike] %s called twice with identical args, both failed (class=%s). "+
				"read the error above carefully — change args or pick a different tool. if blocked, call escalate.\n\n",
				u.Name, class)
			blocks = prependWarningToToolResult(blocks, w)
			e.nudgedTwoStrike = true
			emitRepeatEvent(e, u, "two_strike_nudge", obs)
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

// parseClassFromResult extracts the error class for a failing tool_result.
// Prefers the `[type=...]` tag emitted by formatToolError (set when a tool
// returns a Go error). When the tool returned IsError=true with raw content
// (the common path for sandbox/validation failures), falls back to running the
// classifier on the content string. used to thread the class into
// TwoStrikeError so the exit message isn't `class=` blank.
func parseClassFromResult(content string) string {
	const marker = "[type="
	if i := strings.Index(content, marker); i >= 0 {
		if j := strings.IndexByte(content[i+len(marker):], ']'); j > 0 {
			return content[i+len(marker) : i+len(marker)+j]
		}
	}
	if content == "" {
		return ""
	}
	return classifyToolError(fmt.Errorf("%s", content))
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
