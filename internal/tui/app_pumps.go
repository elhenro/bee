package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// recapTimeout caps the side-call so a stuck provider can't drag the turn
// past the loader fading out. Conservative — most recaps complete in a
// second or two against deepseek-v4-flash class models.
const recapTimeout = 20 * time.Second

// recapIdleDelay defers the side-call until the user has been idle for
// this long after a turn finishes. New submits bump m.recapGen and the
// scheduled tick drops itself when its captured gen no longer matches.
const recapIdleDelay = 30 * time.Second

// quitConfirmWindow is how long a single ctrl+d arms the quit-confirm flow.
// After this elapses, the next ctrl+d re-arms instead of quitting.
const quitConfirmWindow = 2 * time.Second

// streamDeltaMsg carries a single text delta from the engine's StreamCh
// into the bubbletea Update loop.
type streamDeltaMsg struct{ Delta string }

// thinkDeltaMsg carries one chain-of-thought delta from Engine.ThinkCh
// into the bubbletea Update loop so reasoning renders live during a turn.
type thinkDeltaMsg struct{ Delta string }

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

// recapReadyMsg carries the synthesized one-line recap back into Update so
// it can be flushed into scrollback as a dim italic post-turn line. text
// non-empty = render recap; skipped = render "(skip)" diagnostic; err
// non-empty = render error so the user sees why the side-call failed.
type recapReadyMsg struct {
	text    string
	skipped bool
	err     string
}

// recapIdleTickMsg fires recapIdleDelay after a turn finished. gen is the
// m.recapGen value captured when the tick was scheduled — a new submit
// since then bumps m.recapGen so the firing tick drops itself instead of
// kicking off the side-call. msgs is the snapshot to summarise.
type recapIdleTickMsg struct {
	gen  int
	msgs []types.Message
}

// recapIdleCmd schedules a recapIdleTickMsg after recapIdleDelay carrying
// the supplied gen and msgs. tea.Tick can't pass closure data, so we wrap
// time.After in a goroutine-style cmd.
func recapIdleCmd(gen int, msgs []types.Message) tea.Cmd {
	return tea.Tick(recapIdleDelay, func(time.Time) tea.Msg {
		return recapIdleTickMsg{gen: gen, msgs: msgs}
	})
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

// waitThink returns a tea.Cmd that blocks on the next reasoning delta.
// Same re-arming pattern as waitStream — Update re-issues the cmd on
// receipt so the channel keeps draining.
func (m Model) waitThink() tea.Cmd {
	if m.thinkCh == nil {
		return nil
	}
	ch := m.thinkCh
	return func() tea.Msg {
		d, ok := <-ch
		if !ok {
			return nil
		}
		return thinkDeltaMsg{Delta: d}
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

// recapMinDuration / recapMinTextLen gate the recap heuristic. Tuned so a
// quick greeting or one-liner Q&A skips the side call entirely; only turns
// that did real work (tool use, long reply, or wall-clock >= threshold)
// trigger a recap.
const (
	recapMinDuration = 15 * time.Second
	recapMinTextLen  = 600
)

// recapWorthwhile reports whether the just-finished turn merits a recap.
// True if any tool was used, the turn ran long, or the assistant produced
// a substantive reply. Scans the trailing assistant run only — stops at
// the previous user message, matching extractRecapInput's scope.
func recapWorthwhile(msgs []types.Message, dur time.Duration) bool {
	if dur >= recapMinDuration {
		return true
	}
	var textLen int
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == types.RoleUser {
			break
		}
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, c := range m.Content {
			switch c.Type {
			case types.BlockToolUse:
				return true
			case types.BlockText:
				textLen += len(c.Text)
			}
		}
	}
	return textLen >= recapMinTextLen
}

// recapCmd kicks off a side-LLM summarization of the just-finished turn and
// publishes recapReadyMsg with the result. Runs detached from the model's
// long-lived ctx so a cancellation of the next user input doesn't abort the
// recap mid-call; bounded by recapTimeout instead.
func (m Model) recapCmd(msgs []types.Message) tea.Cmd {
	if m.eng == nil || m.eng.Provider == nil {
		return nil
	}
	prov := m.eng.Provider
	model := m.eng.Cfg.DefaultModel
	copyMsgs := append([]types.Message(nil), msgs...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), recapTimeout)
		defer cancel()
		r := loop.GenerateRecap(ctx, prov, model, copyMsgs)
		msg := recapReadyMsg{text: r.Text, skipped: r.Skipped}
		if r.Err != nil {
			msg.err = r.Err.Error()
		}
		return msg
	}
}
