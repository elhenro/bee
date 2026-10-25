package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// quitConfirmWindow is how long a single ctrl+d arms the quit-confirm flow.
// After this elapses, the next ctrl+d re-arms instead of quitting.
const quitConfirmWindow = 2 * time.Second

// streamDeltaMsg carries a single text delta from the engine's StreamCh
// into the bubbletea Update loop.
type streamDeltaMsg struct{ Delta string }

// thinkDeltaMsg carries one chain-of-thought delta from Engine.ThinkCh
// into the bubbletea Update loop so reasoning renders live during a turn.
type thinkDeltaMsg struct{ Delta string }

// progressMsg carries the char count of withheld output from
// Engine.ProgressCh — buffered tool-call modes generate text the user never
// sees mid-stream, but the ↓ figure should still move.
type progressMsg struct{ N int }

// toolTailMsg carries the newest output tail of a running tool from
// Engine.ToolTailCh.
type toolTailMsg struct{ Tail loop.ToolTail }

// liveMsgMsg carries a freshly-persisted message from the engine into the
// TUI so the scrollback updates the moment the loop appends an assistant
// or tool message — instead of waiting for the whole Run to complete.
type liveMsgMsg struct{ Msg types.Message }

// warningMsg carries a transient operational notice from Engine.WarnCh into
// the bubbletea Update loop. The line shows above chrome and fades after
// warningTTL.
type warningMsg struct{ Text string }

// warningFadeMsg fires after warningTTL to clear an active warning.
type warningFadeMsg struct{}

// warningTTL is how long a transient notice stays on screen.
const warningTTL = 5 * time.Second

// loaderTickMsg drives the streaming-loader animation. Self-rearming while
// state == StateStreaming; dies on its own when the turn finishes.
type loaderTickMsg struct{}

// compactDoneMsg is published when an async /compact goroutine finishes.
// nil err means the summarization succeeded. stats carries before/after token
// counts and elapsed time so the success line can show what was achieved.
// msgs is the compacted message slice — the handler swaps m.messages to this
// so subsequent submits send the shorter history. nil msgs (engine no-op or
// short-history path) leaves m.messages untouched.
type compactDoneMsg struct {
	err   error
	stats loop.CompactStats
	msgs  []types.Message
}

// handoffReadyMsg carries the generated rescue brief back into Update after the
// async /handoff summarization goroutine finishes. err non-nil → render the
// error and abort (no model switch). provider/model are the user's pick,
// applied here (not at PickedMsg) so the switch lands only after the brief is
// built on the pre-switch (small) model.
type handoffReadyMsg struct {
	brief    string
	provider string
	model    string
	err      error
}

// loaderTickInterval is the frame cadence. 120ms is fast enough that the
// bee-trail bounce looks alive but slow enough to keep the redraw cost
// invisible on a remote terminal.
const loaderTickInterval = 120 * time.Millisecond

// tickAfter delivers msg once after d.
func tickAfter(d time.Duration, msg tea.Msg) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return msg })
}

func loaderTickCmd() tea.Cmd { return tickAfter(loaderTickInterval, loaderTickMsg{}) }

// glowTickMsg drives the queen rainbow-input animation. Unlike the loader
// tick it runs at idle too — it self-rearms for as long as the queen role is
// active (see onGlowTick) and dies on its own once the role changes.
type glowTickMsg struct{}

// glowTickInterval paces the rainbow hue sweep. 120ms matches the loader so the
// redraw cost stays invisible; the full-hue cycle lands around 7s.
const glowTickInterval = 120 * time.Millisecond

func glowTickCmd() tea.Cmd { return tickAfter(glowTickInterval, glowTickMsg{}) }

// costTickMsg drives the post-turn cost flash animation. Self-rearms while
// costFlashFrame < costFlashUntil; dies on its own once the flash completes.
type costTickMsg struct{}

// costTickInterval paces the post-turn cost fade. Slow enough that the
// badge breathes once instead of strobing.
const costTickInterval = 160 * time.Millisecond

// costFlashDuration is how many frames a single fade plays for. 8 * 160ms ≈
// 1.3s — brief acknowledgement, no nagging shimmer.
const costFlashDuration = 8

func costTickCmd() tea.Cmd { return tickAfter(costTickInterval, costTickMsg{}) }

// introTickMsg advances the non-blocking startup intro animation. Self-rearms
// while introActive; the animation lives above the input bar so typing is
// available from frame zero.
type introTickMsg struct{}

func introTickCmd() tea.Cmd { return tickAfter(introFrameDelay, introTickMsg{}) }

// turnDoneMsg is published when the engine finishes a Run. gen is the
// m.turnGen epoch captured at submit; onTurnDone ignores a msg whose gen no
// longer matches, so a late result from a cancelled/superseded turn can't
// clobber the live one. Mirrors the recapGen / resumeErrGen epoch guards.
type turnDoneMsg struct {
	gen    int
	result loop.RunResult
	err    error
}

// sentinel msgs for unwired panes — slice 3B/3C consume them later.
type (
	openHiveMsg     struct{}
	openProviderMsg struct{}
	openTreeMsg     struct{}
	openPaletteMsg  struct{}
	openCostMsg     struct{}
	openUsageMsg    struct{}
	openLoginMsg    struct{}
	openResumeMsg   struct{}
	openRoleMsg     struct{}
	openEffortMsg   struct{}
	openSettingsMsg struct{}
	openToolsMsg    struct{}
)

// maybeStartCostFlash compares the tracker's call count to what we saw last
// turn; on growth, it captures the cost delta and arms the top-bar flash
// animation. Returns the cmd that drives the first tick (nil = no flash).
func (m *Model) maybeStartCostFlash() tea.Cmd {
	if m.costs == nil {
		return nil
	}
	events := m.costs.Events()
	if len(events) <= m.costPrevCalls {
		return nil
	}
	var delta float64
	for _, e := range events[m.costPrevCalls:] {
		delta += e.USD
	}
	m.costPrevCalls = len(events)
	if delta <= 0 {
		// token usage logged but no priced delta (e.g. local Ollama) — skip
		// the flash so we don't draw attention to a $0 line.
		return nil
	}
	m.costFlashDelta = delta
	m.costFlashFrame = 0
	m.costFlashUntil = costFlashDuration
	return costTickCmd()
}

// pump returns a tea.Cmd that blocks on the next value of ch and wraps it into
// a message. Update re-arms it on receipt so the channel keeps draining. nil
// (a no-op cmd) when no channel is wired.
func pump[T any](ch chan T, msg func(first T) tea.Msg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return nil
		}
		return msg(v)
	}
}

// drainChan collects first plus whatever queued on ch while Update was busy: one
// value per Update cycle can't keep up with fast providers, the buffer fills,
// and the engine's non-blocking send starts dropping deltas (visible text gaps
// mid-stream).
func drainChan[T any](first T, ch chan T) []T {
	out := []T{first}
	for {
		select {
		case more, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, more)
		default:
			return out
		}
	}
}

// waitStream blocks on the next text delta and batch-drains what queued behind it.
func (m Model) waitStream() tea.Cmd {
	ch := m.streamCh
	return pump(ch, func(d string) tea.Msg { return streamDeltaMsg{Delta: strings.Join(drainChan(d, ch), "")} })
}

// waitProgress batch-drains queued withheld-output counts into one msg.
func (m Model) waitProgress() tea.Cmd {
	ch := m.progressCh
	return pump(ch, func(n int) tea.Msg {
		total := 0
		for _, v := range drainChan(n, ch) {
			total += v
		}
		return progressMsg{N: total}
	})
}

// waitThink blocks on the next reasoning delta, same pattern as waitStream.
func (m Model) waitThink() tea.Cmd {
	ch := m.thinkCh
	return pump(ch, func(d string) tea.Msg { return thinkDeltaMsg{Delta: strings.Join(drainChan(d, ch), "")} })
}

// waitLiveMsg blocks on the next mid-Run message from the engine.
func (m Model) waitLiveMsg() tea.Cmd {
	return pump(m.liveMsgCh, func(msg types.Message) tea.Msg { return liveMsgMsg{Msg: msg} })
}

// waitToolTail blocks on the next running-tool tail; only the newest of a
// queued burst matters.
func (m Model) waitToolTail() tea.Cmd {
	ch := m.toolTailCh
	return pump(ch, func(t loop.ToolTail) tea.Msg {
		burst := drainChan(t, ch)
		return toolTailMsg{Tail: burst[len(burst)-1]}
	})
}

// waitWarn blocks on the next transient notice from Engine.WarnCh.
func (m Model) waitWarn() tea.Cmd {
	return pump(m.warnCh, func(w string) tea.Msg { return warningMsg{Text: w} })
}

// warningFadeCmd fires once after warningTTL to clear the displayed line.
func warningFadeCmd() tea.Cmd { return tickAfter(warningTTL, warningFadeMsg{}) }
