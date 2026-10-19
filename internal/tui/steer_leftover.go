package tui

import (
	"strings"

	"github.com/elhenro/bee/internal/loop"
)

// drainSteer empties the engine's steer channel after a turn ends. The loop
// only reads it between iterations, so text typed during the final round is
// still parked there and would be injected into an unrelated later turn.
// Safe: the engine goroutine has already returned.
func drainSteer(eng *loop.Engine) []string {
	if eng == nil || eng.SteerCh == nil {
		return nil
	}
	var left []string
	for {
		select {
		case s := <-eng.SteerCh:
			if s = strings.TrimSpace(s); s != "" {
				left = append(left, s)
			}
		default:
			return left
		}
	}
}
