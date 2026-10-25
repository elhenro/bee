package loop

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// drainSteer folds a pending mid-turn steer into the transcript as a user turn
// before the next LLM round. Non-blocking so quiet iterations don't stall.
func (e *Engine) drainSteer(ctx context.Context, res *RunResult) {
	select {
	case steer := <-e.SteerCh:
		if strings.TrimSpace(steer) != "" && e.push(ctx, res, textMessage(types.RoleUser, lastID(res.Messages), "[steer] "+steer)) == nil {
			e.emitSteerDelivered(steer)
		}
	default:
	}
}

// emitSteerDelivered tells a live UI the model now has a steer. The TUI echoes
// a steer when typed, but the loop only reads it between iterations, so without
// this the user cannot tell sent-and-waiting from seen-by-the-model. Ephemeral:
// UI notice only, never model context or persisted.
func (e *Engine) emitSteerDelivered(text string) {
	note := textMessage(types.RoleAssistant, "", "steer delivered: "+clipRunes(text, 60, "…"))
	note.Ephemeral = true
	trySend(e.LiveMsgCh, note)
}
