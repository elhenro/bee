package loop

import "github.com/elhenro/bee/internal/types"

// emitSteerDelivered tells a live UI the model now has a steer. The TUI echoes
// a steer when typed, but the loop only reads it between iterations, so without
// this the user cannot tell sent-and-waiting from seen-by-the-model. Ephemeral:
// UI notice only, never model context or persisted.
func (e *Engine) emitSteerDelivered(text string) {
	note := textMessage(types.RoleAssistant, "", "steer delivered: "+clipRunes(text, 60, "…"))
	note.Ephemeral = true
	trySend(e.LiveMsgCh, note)
}
