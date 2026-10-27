package tui

import (
	"time"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// engineFeeds holds the engine channels the Update loop drains and the live buffers
// they fill, embedded in Model.
type engineFeeds struct {
	// streamCh receives text deltas from the engine via Engine.StreamCh.
	// nil in tests; lifetime owned by the caller of WithStreamCh.
	streamCh chan string

	// progressCh receives withheld-output char counts via Engine.ProgressCh
	// (buffered tool-call modes). Feeds turnOutChars when streamCh is silent.
	progressCh chan int

	// thinkCh receives chain-of-thought deltas from the engine via
	// Engine.ThinkCh so reasoning renders live above the answer instead
	// of dumping all at once when the stream ends.
	thinkCh chan string
	// thinkPartial is the accumulated reasoning buffer for the in-flight
	// turn. Rendered dim+italic above m.partial while streaming. Cleared
	// when the final BlockThinking lands via liveMsgMsg/turnDoneMsg.
	thinkPartial string
	// reasoning clock for the live row: set by the first delta of a block,
	// frozen when the answer starts (see think_time.go)
	thinkStartedAt, thinkEndedAt time.Time

	// liveMsgCh receives each persisted assistant/tool message from the
	// engine's LiveMsgCh so cards render mid-Run instead of only on done.
	liveMsgCh chan types.Message

	// toolTailCh receives the output tail of a running tool via
	// Engine.ToolTailCh; liveTail is the latest one, shown under its pending row.
	toolTailCh chan loop.ToolTail
	liveTail   loop.ToolTail

	// warnCh receives transient operational notices from Engine.WarnCh —
	// stream retries, watchdog hiccups, etc. The latest one shows as a dim
	// line, then fades after warningTTL.
	warnCh chan string
	// warning is the currently-displayed notice ("" when none active).
	warning string
	// warningExpires is the wall-clock instant after which the warning fades.
	warningExpires time.Time
}

// flushState tracks the progressive head-flush of the streaming reply into
// scrollback, embedded in Model.
type flushState struct {
	// streamFlushed holds the prefix of m.partial that has already been
	// pushed into terminal scrollback via tea.Println by the progressive
	// flush path. View() renders only m.partial[len(streamFlushed):]; flush()
	// strips the prefix from the matching final assistant message so the
	// settled head doesn't get re-printed underneath.
	streamFlushed string
	// streamFenceOpen tracks whether the already-flushed prefix sits inside
	// an unclosed ``` fence. Drives the glamour-vs-raw rendering choice for
	// the next chunk: code-block content gets shipped raw (preserving its
	// monospace layout) while prose chunks flow through glamour for full
	// markdown styling (headings, lists, bold, links).
	streamFenceOpen bool
	// pendingFlushedPrefix carries streamFlushed across the partial reset
	// (turnDoneMsg / liveMsgMsg) so the next flush() call can suppress the
	// already-printed prefix. Consumed (cleared) on first flush() call after
	// commit, even if no matching message was found.
	pendingFlushedPrefix string
}

// promptCycle is the up/down prompt history cycling state, embedded in Model.
type promptCycle struct {
	// up/down prompt cycling. cycleActive is true mid-cycle; cycleEntries is
	// the deduped newest-first list loaded once per cycle session (includes
	// prior sessions via ~/.bee/history); cycleIdx is -1 before the first
	// up press; cycleStash holds the buffer the user had typed before
	// cycling started so down past the newest restores it.
	cycleActive  bool
	cycleEntries []string
	cycleStash   string
	cycleIdx     int
}
