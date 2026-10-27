package loop

import (
	"context"

	"github.com/elhenro/bee/internal/types"
)

// streamRecovering is streamOnce plus one recovery from a context overflow
// rejection: compact the history on the spot and send the request again. When
// compaction is off, fails or cannot shrink anything, the raw error surfaces.
// Ignores the compactFailed latch, since an overflow is dead without a try.
func (e *Engine) streamRecovering(ctx context.Context, pl runPlan, res *RunResult) (types.Message, string, []types.ToolUse, error) {
	msg, text, uses, err := e.streamOnce(ctx, e.buildRequest(ctx, pl, res.Messages))
	if !isContextOverflowErr(err) || !e.compactionEnabled() {
		return msg, text, uses, err
	}
	msgs, shrunk := e.compactNow(ctx, res.Messages)
	if !shrunk {
		return msg, text, uses, err
	}
	e.noticef("context window exceeded: compacted history, retrying")
	res.Messages = msgs
	return e.streamOnce(ctx, e.buildRequest(ctx, pl, res.Messages))
}
