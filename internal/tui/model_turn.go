package tui

import (
	"time"

	"github.com/elhenro/bee/internal/types"
)

// turnState is the in-flight turn bookkeeping, embedded in Model.
type turnState struct {
	// pendingTools holds the tool_use blocks of the in-flight turn: set when
	// the assistant message lands (before dispatch), cleared when the tool
	// result message arrives. The live region renders each as a per-tool
	// swarm so a batch of calls shows as N buzzing bees while they run,
	// instead of one generic loader.
	pendingTools []types.ToolUse
	// pendingSince is when that batch landed; the busy row shows how long
	// the model has been waiting on it
	pendingSince time.Time

	// turnGen is a monotonic epoch bumped on every submit and stamped onto the
	// turn's turnDoneMsg. A late result from a cancelled turn (user esc) carries
	// a stale gen and is dropped by onTurnDone, so it can't nil cancelRun, flip
	// state to idle, or overwrite messages of a newer turn.
	turnGen int
	// runDone closes when the in-flight engine goroutine returns. A resubmit
	// waits on the prior run's runDone before touching the shared engine, so an
	// esc'd turn still unwinding never runs concurrently with the next one — the
	// manual-cancel analogue of the watchdog's wait-for-landing handshake.
	runDone chan struct{}

	// queue holds follow-up messages submitted via Alt+Enter; drained
	// one-at-a-time when each turn finishes.
	queue []string

	// esc was pressed on this turn: when its Canceled turnDoneMsg lands, say so
	interrupted bool
	// the last printed tool card cut rows: the footer offers ctrl+o
	lastToolClipped bool
}
