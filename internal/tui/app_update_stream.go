package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/types"
)

func (m Model) onStreamDelta(msg streamDeltaMsg) (tea.Model, tea.Cmd) {
	// append to live partial. View() picks it up next render. The pump
	// re-arms itself so subsequent deltas keep draining.
	m.partial += msg.Delta
	// newline-gated head flush: only check the budget when this delta
	// completed a line. Tiny per-character deltas skip the work; line
	// terminators trigger the overflow check + possible scrollback push.
	var flushCmd tea.Cmd
	if strings.ContainsRune(msg.Delta, '\n') {
		flushCmd = m.maybeFlushPartialHead()
	}
	if flushCmd == nil {
		return m, m.waitStream()
	}
	return m, tea.Batch(flushCmd, m.waitStream())
}

func (m Model) onLiveMsg(msg liveMsgMsg) (tea.Model, tea.Cmd) {
	// engine persisted a new assistant/tool message mid-Run; print it to
	// native scrollback right away so the user sees tool cards as they
	// happen instead of only at turnDoneMsg. clear m.partial because the
	// assistant's text is now part of the appended ContentBlock —
	// leaving the live buffer would double-render the same text. Dedupe
	// by ID so a turnDoneMsg replacement followed by a late-arriving
	// live msg doesn't double-add.
	if msg.Msg.ID != "" {
		for _, existing := range m.messages {
			if existing.ID == msg.Msg.ID {
				return m, m.waitLiveMsg()
			}
		}
	}
	m.messages = append(m.messages, msg.Msg)
	m.commitFlushed()
	m.partial = ""
	flushCmd := m.flush()
	return m, tea.Batch(flushCmd, m.waitLiveMsg())
}

func (m Model) onWarning(msg warningMsg) (tea.Model, tea.Cmd) {
	// transient notice from the loop (stream retry, watchdog stall).
	// Show the latest one; arm a fade tick to clear it. Re-arm the
	// channel pump so subsequent notices also surface.
	m.warning = msg.Text
	m.warningExpires = time.Now().Add(warningTTL)
	return m, tea.Batch(warningFadeCmd(), m.waitWarn())
}

func (m Model) onWarningFade(_ warningFadeMsg) (tea.Model, tea.Cmd) {
	// only clear if no newer warning has bumped the expiry forward.
	if !m.warningExpires.IsZero() && !time.Now().Before(m.warningExpires) {
		m.warning = ""
		m.warningExpires = time.Time{}
	}
	return m, nil
}

func (m Model) onLoaderTick(_ loaderTickMsg) (tea.Model, tea.Cmd) {
	// Only animate while a turn or async compact is in flight. Letting the
	// tick die when we leave streaming/compacting keeps idle terminals quiet.
	if m.state != StateStreaming && !m.compacting {
		return m, nil
	}
	m.loaderFrame++
	return m, loaderTickCmd()
}

func (m Model) onCompactDone(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	m.compacting = false
	if msg.err != nil {
		m.lastErr = msg.err.Error()
		m.state = StateError
		return m, nil
	}
	m.messages = append(m.messages, types.Message{
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: formatCompactDone(msg.stats)}},
	})
	return m, m.flush()
}

func (m Model) onTurnDone(msg turnDoneMsg) (tea.Model, tea.Cmd) {
	m.cancelRun = nil
	// freeze elapsed at turn end. Guard zero turnStartedAt — late msgs
	// after Model reset shouldn't synthesize a huge duration.
	if !m.turnStartedAt.IsZero() {
		m.lastTurnDuration = time.Since(m.turnStartedAt)
		m.turnStartedAt = time.Time{}
	}
	switch {
	case errors.Is(msg.err, context.Canceled):
		// user pressed esc — clean cancel, not a failure. preserve any
		// messages the engine flushed before the cancel landed so the
		// scrollback isn't blanked, and stay idle (StateError would
		// gate the `/` palette and `@` picker behind error-recovery).
		if len(msg.result.Messages) > 0 {
			m.messages = msg.result.Messages
		}
		m.commitFlushed()
		m.partial = ""
		m.state = StateIdle
	case msg.err != nil:
		// drop any progressively-flushed prefix on error — there's no
		// final assistant message to dedupe against, just clear state.
		m.streamFlushed = ""
		m.streamFenceOpen = false
		m.pendingFlushedPrefix = ""
		m.state = StateError
		m.lastErr = msg.err.Error()
	default:
		m.messages = msg.result.Messages
		m.commitFlushed()
		m.partial = ""
		m.state = StateIdle
	}
	flushCmd := m.flush()
	// kick off the top-bar cost flash when a fresh event landed. Diff
	// the call-count against the previous turn so multi-iteration loops
	// fold all their per-iteration events into one visible delta.
	costCmd := m.maybeStartCostFlash()
	// drain one queued follow-up per turn so the TUI stays responsive
	// between fires. Only when last turn didn't error.
	if msg.err == nil && len(m.queue) > 0 && m.eng != nil {
		nxt := m.queue[0]
		m.queue = m.queue[1:]
		nm, runCmd := m.submit(nxt)
		return nm, tea.Batch(flushCmd, costCmd, runCmd)
	}
	return m, tea.Batch(flushCmd, costCmd)
}

func (m Model) onCostTick(_ costTickMsg) (tea.Model, tea.Cmd) {
	if m.costFlashFrame >= m.costFlashUntil {
		m.costFlashUntil = 0
		return m, nil
	}
	m.costFlashFrame++
	return m, costTickCmd()
}

func (m Model) onIntroTick(_ introTickMsg) (tea.Model, tea.Cmd) {
	if !m.introActive {
		return m, nil
	}
	// build frames on first tick when width is finally known. If width
	// still hasn't arrived (initial WindowSizeMsg pending), just rearm.
	if m.introFrames == nil {
		if m.width <= 0 {
			return m, introTickCmd()
		}
		m.introFrames = introFrames(m.introStyle, m.width)
		if len(m.introFrames) == 0 {
			m.introActive = false
			return m, nil
		}
	}
	m.introIdx++
	if m.introIdx >= len(m.introFrames) {
		m.introActive = false
		m.introFrames = nil
		return m, nil
	}
	return m, introTickCmd()
}
