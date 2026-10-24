package loop

import (
	"time"

	"github.com/elhenro/bee/internal/types"
)

// emitSteerDelivered tells a live UI the model now has a steer. The TUI echoes
// a steer when typed, but the loop only reads it between iterations, so without
// this the user cannot tell sent-and-waiting from seen-by-the-model. Ephemeral:
// UI notice only, never model context or persisted.
func (e *Engine) emitSteerDelivered(text string) {
	if e.LiveMsgCh == nil {
		return
	}
	note := types.Message{
		ID:        newID(),
		Role:      types.RoleAssistant,
		Content:   []types.ContentBlock{{Type: types.BlockText, Text: "steer delivered: " + steerPreview(text)}},
		Time:      time.Now().UTC(),
		Ephemeral: true,
	}
	select {
	case e.LiveMsgCh <- note:
	default:
	}
}

func steerPreview(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}
