package tui

import (
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
type compactDoneMsg struct {
	err   error
	stats loop.CompactStats
}

// loaderTickInterval is the frame cadence. 120ms is fast enough that the
// bee-trail bounce looks alive but slow enough to keep the redraw cost
// invisible on a remote terminal.
const loaderTickInterval = 120 * time.Millisecond

func loaderTickCmd() tea.Cmd {
	return tea.Tick(loaderTickInterval, func(time.Time) tea.Msg { return loaderTickMsg{} })
}

// costTickMsg drives the post-turn cost flash animation. Self-rearms while
// costFlashFrame < costFlashUntil; dies on its own once the flash completes.
type costTickMsg struct{}

// costTickInterval paces the post-turn cost fade. Slow enough that the
// badge breathes once instead of strobing.
const costTickInterval = 160 * time.Millisecond

// costFlashDuration is how many frames a single fade plays for. 8 * 160ms ≈
// 1.3s — brief acknowledgement, no nagging shimmer.
const costFlashDuration = 8

func costTickCmd() tea.Cmd {
	return tea.Tick(costTickInterval, func(time.Time) tea.Msg { return costTickMsg{} })
}

// introTickMsg advances the non-blocking startup intro animation. Self-rearms
// while introActive; the animation lives above the input bar so typing is
// available from frame zero.
type introTickMsg struct{}

func introTickCmd() tea.Cmd {
	return tea.Tick(introFrameDelay, func(time.Time) tea.Msg { return introTickMsg{} })
}

// turnDoneMsg is published when the engine finishes a Run.
type turnDoneMsg struct {
	result loop.RunResult
	err    error
}

// sentinel msgs for unwired panes — slice 3B/3C consume them later.
type (
	openWorkspaceMsg struct{}
	openHiveMsg      struct{}
	openProviderMsg  struct{}
	openTreeMsg      struct{}
	openPaletteMsg   struct{}
	openCostMsg      struct{}
	openLoginMsg     struct{}
	openResumeMsg    struct{}
	openEffortMsg    struct{}
	openSettingsMsg  struct{}
	openToolsMsg     struct{}
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

// waitStream returns a tea.Cmd that blocks on the next text delta. The
// pump re-arms itself in Update so the channel keeps draining. Returns
// nil (a no-op cmd) when no channel is wired.
func (m Model) waitStream() tea.Cmd {
	if m.streamCh == nil {
		return nil
	}
	ch := m.streamCh
	return func() tea.Msg {
		d, ok := <-ch
		if !ok {
			return nil
		}
		return streamDeltaMsg{Delta: d}
	}
}

// waitLiveMsg blocks on the next mid-Run message from the engine. Same
// re-arming pattern as waitStream.
func (m Model) waitLiveMsg() tea.Cmd {
	if m.liveMsgCh == nil {
		return nil
	}
	ch := m.liveMsgCh
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return liveMsgMsg{Msg: msg}
	}
}

// waitWarn blocks on the next transient notice from Engine.WarnCh. Same
// re-arming pattern as waitStream — Update re-issues the cmd on receipt.
func (m Model) waitWarn() tea.Cmd {
	if m.warnCh == nil {
		return nil
	}
	ch := m.warnCh
	return func() tea.Msg {
		w, ok := <-ch
		if !ok {
			return nil
		}
		return warningMsg{Text: w}
	}
}

// warningFadeCmd fires once after warningTTL to clear the displayed line.
func warningFadeCmd() tea.Cmd {
	return tea.Tick(warningTTL, func(time.Time) tea.Msg { return warningFadeMsg{} })
}
