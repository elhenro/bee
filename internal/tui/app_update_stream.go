package tui

import (
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/types"
)

// onThinkDelta accumulates a reasoning delta into m.thinkPartial so the
// dim/italic CoT block renders live during streaming. The pump re-arms
// itself; no flush — thinkPartial clears when the BlockThinking content
// block lands via liveMsgMsg/turnDoneMsg (the final renders from the
// message, the partial is just for the in-flight view).
func (m Model) onThinkDelta(msg thinkDeltaMsg) (tea.Model, tea.Cmd) {
	m.noteActivity()
	m.noteThinking()
	m.thinkPartial += msg.Delta
	m.turnOutChars += len(msg.Delta)
	return m, m.waitThink()
}

// onProgress accumulates withheld-output chars (buffered tool-call modes)
// into turnOutChars so the ↓ figure and throughput readout stay live even
// when no displayable text deltas flow.
func (m Model) onProgress(msg progressMsg) (tea.Model, tea.Cmd) {
	m.noteActivity()
	m.turnOutChars += msg.N
	return m, m.waitProgress()
}

func (m Model) onStreamDelta(msg streamDeltaMsg) (tea.Model, tea.Cmd) {
	m.noteActivity()
	m.answerStarted()
	// append to live partial. View() picks it up next render. The pump
	// re-arms itself so subsequent deltas keep draining.
	m.partial += msg.Delta
	m.turnOutChars += len(msg.Delta)
	// newline-gated head flush: only check the budget when this delta
	// completed a line. Tiny per-character deltas skip the work; line
	// terminators trigger the overflow check + possible scrollback push.
	var flushCmd tea.Cmd
	if strings.ContainsRune(msg.Delta, '\n') {
		flushCmd = m.maybeFlushPartialHead()
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
	m.noteActivity()
	if id := msg.Msg.ID; id != "" && slices.ContainsFunc(m.messages, func(e types.Message) bool { return e.ID == id }) {
		return m, m.waitLiveMsg()
	}
	m.messages = append(m.messages, msg.Msg)
	// track in-flight tools for the live-region swarm: an assistant turn
	// carrying tool_use blocks is about to dispatch; the following tool
	// message means that batch finished, so clear.
	switch msg.Msg.Role {
	case types.RoleAssistant:
		m.pendingTools = pendingToolUses(msg.Msg)
		m.pendingSince = time.Now()
	case types.RoleTool:
		m.pendingTools = nil
	}
	m.commitFlushed()
	m.partial = ""
	// reasoning block now lives in m.messages; clear the live partial so
	// scrollback doesn't double-render it next View().
	m.thinkPartial = ""
	// thinking block just landed — reset loader rate tracking so the
	// token-strip's first frame after the answer starts renders at the
	// floor (3 particles) instead of the per-tick thinking char rate,
	// which would otherwise burst in for a single frame at the
	// thinking→answer transition. The braille loader that animates
	// during thinking is unaffected (it doesn't read these fields); the
	// cumulative ↓ figure also keeps including reasoning by design.
	if hasThinkingBlock(msg.Msg) {
		m.resetLoaderRate(m.turnOutChars)
	}
	flushCmd := m.flush()
	return m, tea.Batch(flushCmd, m.waitLiveMsg())
}

// resetLoaderRate zeroes the throughput readout; sampleChars is the new
// baseline the next tick measures growth from.
func (m *Model) resetLoaderRate(sampleChars int) {
	m.loaderRate, m.loaderRateEMA, m.loaderRateTokS = 0, 0, 0
	m.loaderSampleChars = sampleChars
	m.loaderRateSamples = m.loaderRateSamples[:0]
}

// pendingToolUses extracts the tool_use blocks from an assistant message so
// the live region can render them as in-flight swarms. Returns nil when the
// turn carried no calls (pure text/reasoning) — the generic loader shows.
func pendingToolUses(m types.Message) []types.ToolUse {
	var out []types.ToolUse
	for _, b := range m.Content {
		if b.Type == types.BlockToolUse && b.Use != nil {
			out = append(out, *b.Use)
		}
	}
	return out
}

// hasThinkingBlock reports whether m carries a chain-of-thought content
// block. Used to gate the loader-rate reset on the thinking→answer
// transition so a pure-text turn doesn't drop its accumulated rate.
func hasThinkingBlock(m types.Message) bool {
	return slices.ContainsFunc(m.Content, func(b types.ContentBlock) bool { return b.Type == types.BlockThinking })
}

func (m Model) onWarning(msg warningMsg) (tea.Model, tea.Cmd) {
	// transient notice from the loop (stream retry, watchdog stall).
	// Show the latest one; arm a fade tick to clear it. Re-arm the
	// channel pump so subsequent notices also surface. A retry notice is
	// real progress signal — bump the stall clock so a recovering stream
	// isn't double-killed by the watchdog.
	m.noteActivity()
	m = m.withWarn(msg.Text)
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

func (m Model) onCompactDone(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	m.compacting = false
	if msg.err != nil {
		return m.setError(msg.err.Error())
	}
	// swap in the compacted history so the next submit's engine.InitialMessages
	// (rebuilt from m.messages in handleSubmit) carries the shorter slice.
	// nil msgs = engine no-op (no session, no shrink) — leave m.messages alone.
	if len(msg.msgs) > 0 {
		m.messages = append([]types.Message(nil), msg.msgs...)
		// the swapped-in slice is shorter than the printed scrollback, so the old
		// printedCount now points past its end. re-anchor to the new length: the
		// original (longer) messages were already Println'd into the terminal, so
		// flush() should only emit the tail from here. without this, flush() hits
		// its printedCount > len re-anchor branch and silently drops the
		// "(/compact done …)" summary appended just below.
		m.printedCount = len(m.messages)
		if m.eng != nil {
			m.eng.InitialMessages = nil
			// nudge the context-fill indicator down immediately — without
			// this, the % stays frozen at the pre-compact value until the
			// next assistant turn lands and overwrites LastInput.
			if m.eng.Costs != nil && msg.stats.AfterTokens > 0 {
				m.eng.Costs.SetEstimatedInput(msg.stats.AfterTokens)
			}
		}
	}
	m.messages = append(m.messages, assistantText(formatCompactDone(msg.stats)))
	// drain mid-compact queue: a "continue" typed while compacting is held
	// until now so it runs against the shrunken history. submit() pushes its
	// own messages + cmds — batch the prior flush with whatever it returns.
	if q := m.queuedMidCompact; q != "" {
		m.queuedMidCompact = ""
		flushed := m.flush()
		newM, subCmd := m.submit(q)
		return newM, tea.Batch(flushed, subCmd)
	}
	return m, m.flush()
}
