package bench

import (
	"github.com/elhenro/bee/internal/types"
)

// RunMetrics is what we mine from a run's session transcript. Pure counting —
// no judgement here.
type RunMetrics struct {
	Turns        int // assistant messages
	ToolCalls    int // tool_use blocks
	ErroredCalls int // tool_result blocks with is_error (unknown tool, bad input, failed exec)
	StoppedClean bool
	// WallMillis is the task's end-to-end wall-clock in ms (mean across repeats
	// when Runs>1). Observational only — never feeds Score — but it's the one
	// signal that exposes latency wins (prompt cache, keep-warm, sync probe);
	// without it a tuner can't tell a 4s turn from a 40s one.
	WallMillis int64
}

// MetricsFromMessages counts turns and tool activity from a transcript.
// stoppedClean reflects whether the goal loop ended on its own ("✓ goal
// achieved" / clean stop) vs hitting a cap — the caller knows that, so it is
// passed in rather than inferred from messages.
func MetricsFromMessages(msgs []types.Message, stoppedClean bool) RunMetrics {
	m := RunMetrics{StoppedClean: stoppedClean}
	for _, msg := range msgs {
		if msg.Role == types.RoleAssistant {
			m.Turns++
		}
		for _, b := range msg.Content {
			switch b.Type {
			case types.BlockToolUse:
				m.ToolCalls++
			case types.BlockToolResult:
				if b.Result != nil && b.Result.IsError {
					m.ErroredCalls++
				}
			}
		}
	}
	return m
}
