package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// handleSubmit dispatches the current input. A leading "/" routes to the
// command registry; anything else feeds the LLM via submit(). Empty input
// or non-idle state are silent no-ops.
func (m Model) handleSubmit() (tea.Model, tea.Cmd) {
	// allow recovery from StateError: clear the error and proceed. blocking
	// here was the root cause of "Enter does nothing after max-iter abort".
	if m.state == StateError {
		// preserve the bail reason for a following /handoff before clearing it —
		// the brief's stall signal would otherwise be lost on recovery.
		m.handoffStall = m.lastErr
		m.state = StateIdle
		m.lastErr = ""
		// user is driving again — drop the auto-resume budget and any pending
		// stall handshake so the watchdog doesn't fight the manual recovery.
		m.resumeCount = 0
		m.awaitingProgress = false
		m.stallResumePending = false
	}
	// /compact runs async with state=StateIdle (loader driven by m.compacting).
	// Without this guard, submit() mid-compact would race the engine —
	// RunWithContent would kick off while Compact still mutates session state.
	// Queue plain-text submits so "continue" typed mid-compact lands once the
	// compacted history is in place. Slash commands and shell-bangs aren't
	// queued (meta, weird to delay) — they get a hint and are dropped.
	if m.compacting {
		text := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		if text == "" {
			return m, nil
		}
		if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!") {
			m.messages = append(m.messages, ephemeral("(compacting, slash/shell commands not queued)"))
			return m, m.flush()
		}
		// queue lives in m.queuedMidCompact only — renderLive paints it
		// inline below the compacting animation as a blue "queued: <text>"
		// line so it sits right above the input bar. No scrollback echo:
		// the live line vanishes when onCompactDone drains the queue.
		m.queuedMidCompact = text
		return m, nil
	}
	if m.state != StateIdle {
		// mid-run slash commands: allow read-only ones (model, cost, tree,
		// help, settings, effort) so the user can flip settings or open a
		// picker without waiting for the turn to finish. Anything else is a
		// no-op with a transient hint.
		text := strings.TrimSpace(m.input.Value())
		if !strings.HasPrefix(text, "/") {
			return m, nil
		}
		name, _, _ := strings.Cut(text[1:], " ")
		if m.cmds != nil {
			if c, ok := m.cmds.Get(name); ok && c.AllowDuringRun {
				m.input.Reset()
				// preserve streaming state across slashFail — runSlash flips to
				// StateError on bad input which would silently kill the turn.
				prev := m.state
				newM, cmd := m.runSlash(text)
				if mm, ok := newM.(Model); ok && mm.state == StateError {
					mm.state = prev
					mm.lastErr = ""
					return mm, cmd
				}
				return newM, cmd
			}
		}
		m.input.Reset()
		m.messages = append(m.messages, ephemeral("(/"+name+" unavailable while bee is running)"))
		return m, m.flush()
	}
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	// record every accepted submission for ctrl+r reverse search.
	AppendHistory(text)
	// the cycle list predates this submit and its index points into it
	m.resetCycle()

	// inline shell: ! follows the user's configured default (cfg.ShellBangSilent
	// — silent by default so quick lookups don't burn tokens). !! inverts the
	// default, giving a per-invocation escape hatch in either direction.
	if cmd, count, isInline := parseInlinePrefix(text); isInline {
		if m.eng == nil {
			return m, nil
		}
		silent := resolveBangSilent(m.shellBangSilent, count)
		return m, m.inlineShellCmd(cmd, silent)
	}

	if strings.HasPrefix(text, "/") {
		return m.runSlash(text)
	}
	return m.submit(text)
}

// nonEphemeral copies msgs, dropping scrollback-only UI echoes (slash
// confirmations, queue/steer notices). Without this the model sees an
// assistant turn like "(/new done)" replayed in context and parrots it back as
// a bogus finish signal, ending turns early without doing the work.
func nonEphemeral(msgs []types.Message) []types.Message {
	return slices.DeleteFunc(slices.Clone(msgs), func(m types.Message) bool { return m.Ephemeral })
}

// submit records the user message locally and kicks off engine.Run in a goroutine.
// The result comes back via turnDoneMsg.
func (m Model) submit(text string) (tea.Model, tea.Cmd) {
	return m.submitWithDisplay(text, "")
}

// submitWithDisplay submits text to the engine but renders the user bubble as
// display when non-empty. Used by slash skills so "/plan build X" shows in
// scrollback while the expanded skill body (text) goes to the model.
func (m Model) submitWithDisplay(text, display string) (tea.Model, tea.Cmd) {
	m.state = StateStreaming
	m.interrupted = false
	m.lastToolClipped = false
	// a fresh turn supersedes any stashed bail reason — a later /handoff must
	// not inherit a stall from an already-recovered failure.
	m.handoffStall = ""
	// fresh stream — drop any lingering flush state from a previous turn so
	// the next progressive flush starts clean.
	m.dropFlushed()
	m.partial = ""
	m.loaderFrame = 0
	// fresh token-stream loader: zero the live output counters and roll a new
	// procedural seed so this generation's particle layout is distinct.
	m.turnOutChars = 0
	if m.costs != nil {
		m.turnStartOutput = m.costs.Total().Output
	}
	m.resetLoaderRate(0)
	m.loaderSeed = time.Now().UnixNano()
	// stamp turn start; clear last duration so the timer chip switches from
	// "final" to "live" mode immediately, no stale final reading lingering.
	m.turnStartedAt = time.Now()
	m.lastTurnDuration = 0
	// invalidate any pending post-turn recap tick from the previous turn.
	m.recapGen++
	// watchdog: stamp activity so the stall clock starts fresh, re-arm
	// first-token grace (no stall-resume until this turn shows life), and
	// invalidate any pending error-resume tick (a new turn supersedes it).
	m.lastActivityAt = time.Now()
	m.turnSawActivity = false
	m.resumeErrGen++

	// build content blocks: text first, then a pending image if staged.
	// dragged/typed image file paths load as image blocks; the path token is
	// swapped for a compact "[Image: name]" label in both the model text and
	// the scrollback display.
	content := []types.ContentBlock{{Type: types.BlockText, Text: text}}
	// fallback for terminals without bracketed paste: a raw image path typed
	// into the buffer is still resolved at submit (handlePaste covers the
	// common drag case and leaves a label that won't re-match here).
	if clean, imgs := extractImagePaths(text); len(imgs) > 0 {
		content = append([]types.ContentBlock{{Type: types.BlockText, Text: clean}}, imgs...)
		if display == "" {
			display = clean
		}
	}
	// images staged from dragged paths (handlePaste) and Ctrl+I clipboard.
	content = append(content, m.pendingImages...)
	m.pendingImages = nil
	if len(m.pendingImage) > 0 {
		content = append(content, types.ContentBlock{
			Type:      types.BlockImage,
			MediaType: "image/png",
			Data:      m.pendingImage,
		})
		m.pendingImage = nil
	}

	// optimistic user message in scrollback
	m.messages = append(m.messages, types.Message{
		Role:    types.RoleUser,
		Content: content,
		Display: display,
	})
	userFlush := m.flush()

	// stamp this turn with a monotonic epoch so a late result from a previously
	// cancelled turn (user esc) is ignored by onTurnDone instead of clobbering
	// this one. serialize the engine goroutine behind the prior run's completion
	// (prevDone) so an esc'd turn still unwinding never mutates the shared engine
	// concurrently with this one — the manual-cancel analogue of the watchdog's
	// wait-for-landing handshake. done closes when this run's goroutine returns.
	m.turnGen++
	gen := m.turnGen
	prevDone := m.runDone
	done := make(chan struct{})
	m.runDone = done

	if m.eng == nil {
		// no engine wired (tests): synthesize an echo turn so state still advances
		merged := append(slices.Clone(m.messages), assistantText("(no engine: "+text+")"))
		return m, tea.Batch(
			userFlush,
			func() tea.Msg {
				defer close(done)
				if prevDone != nil {
					<-prevDone
				}
				return turnDoneMsg{gen: gen, result: loop.RunResult{Messages: merged}}
			},
			m.loaderTick(),
		)
	}

	ctx, cancel := context.WithCancel(m.baseCtx())
	m.cancelRun = cancel
	eng := m.eng
	toggles := m.cfgSnapshot()
	// prior turns for context. exclude the optimistic user msg we just appended
	// (history); prior keeps it (the full list the queen run rebuilds its result from).
	var history []types.Message
	if n := len(m.messages); n > 0 {
		history = nonEphemeral(m.messages[:n-1])
	}
	// queen role: route the turn through a sub-agent hive instead of a
	// single engine Run. The glow tick keeps the rainbow input alive mid-turn.
	if m.role == "queen" {
		// the glow tick is already running (armed when the role was enabled and
		// self-rearming across turns), so don't arm a second loop here.
		prior := append([]types.Message(nil), m.messages...)
		return m, tea.Batch(
			userFlush,
			m.runQueen(ctx, gen, prevDone, done, content, history, prior),
			m.loaderTick(),
		)
	}
	return m, tea.Batch(
		userFlush,
		func() tea.Msg {
			defer close(done)
			if prevDone != nil {
				<-prevDone // wait for a prior (possibly esc'd) run to fully return
			}
			// toggles flipped mid-turn reach the engine only now that the prior
			// run released it.
			toggles.apply(eng)
			// seed prior turns into the engine here, after the prior run released
			// it, so two runs never write InitialMessages concurrently.
			// StripBrokenToolCall scrubs the trailing assistant message when it
			// ends on a tool_use with no matching tool_result — a leftover from
			// a turn the provider rejected for malformed arguments. the
			// aggressive variant drops the whole message (including the long
			// text preamble that drove the wedge) instead of just the tool_use
			// block, so the next attempt doesn't re-derive the same broken
			// approach. no-op when the last message is fine.
			eng.InitialMessages = loop.StripBrokenToolCall(history)
			res, err := eng.RunWithContentDisplay(ctx, content, display)
			return turnDoneMsg{gen: gen, result: res, err: err}
		},
		m.loaderTick(),
	)
}
