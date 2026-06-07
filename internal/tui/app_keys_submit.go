package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// handleImagePaste reads the system clipboard and stages an image for the
// next submit. Only fires when idle — staging mid-stream would race with the
// running engine's message slice. Failure (no image, headless clipboard)
// surfaces via the standard error line.
func (m Model) handleImagePaste() (tea.Model, tea.Cmd) {
	if m.state != StateIdle {
		return m, nil
	}
	img, err := ReadClipboardImage()
	if err != nil {
		m.lastErr = "clipboard: " + err.Error()
		m.state = StateError
		return m, nil
	}
	m.pendingImage = img
	m.messages = append(m.messages, types.Message{
		Role: types.RoleAssistant,
		Content: []types.ContentBlock{{
			Type: types.BlockText,
			Text: "(image staged: " + bytesHuman(len(img)) + ")",
		}},
		Ephemeral: true,
	})
	return m, m.flush()
}

// handlePaste intercepts a bracketed paste. When the pasted text is (or
// contains) a local image file path, it stages the image(s) and inserts a
// compact "[Image: name]" label into the input instead of the raw path.
// Ordinary text pastes fall through to the textarea unchanged.
func (m Model) handlePaste(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pasted := string(msg.Runes)
	// common case: the whole paste is one dragged file (handles unescaped
	// spaces a single token-scan would split).
	if blk, base, ok := loadImage(unescape(pasted)); ok {
		m.pendingImages = append(m.pendingImages, blk)
		m.input.InsertString(imageLabel(base))
		return m, nil
	}
	// else: scan for image path tokens embedded in surrounding text.
	if clean, imgs := extractImagePaths(pasted); len(imgs) > 0 {
		m.pendingImages = append(m.pendingImages, imgs...)
		m.input.InsertString(clean)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// handleFollowUp pushes the current input onto the follow-up queue. Fires
// regardless of state — queued runs kick off as the current turn finishes.
func (m Model) handleFollowUp() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	m.queue = append(m.queue, text)
	m.messages = append(m.messages, types.Message{
		Role:      types.RoleAssistant,
		Content:   []types.ContentBlock{{Type: types.BlockText, Text: "(queued: " + text + ")"}},
		Ephemeral: true,
	})
	return m, m.flush()
}

// handleSteer pushes the current input into the engine's SteerCh so the
// running Run loop picks it up between iterations. Drops with a note if
// the channel is full or the engine isn't wired for steering.
func (m Model) handleSteer() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	if m.eng == nil || m.eng.SteerCh == nil {
		m.messages = append(m.messages, types.Message{
			Role:      types.RoleAssistant,
			Content:   []types.ContentBlock{{Type: types.BlockText, Text: "(steer dropped: no channel)"}},
			Ephemeral: true,
		})
		return m, m.flush()
	}
	select {
	case m.eng.SteerCh <- text:
		m.messages = append(m.messages, types.Message{
			Role:      types.RoleAssistant,
			Content:   []types.ContentBlock{{Type: types.BlockText, Text: "(steering: " + text + ")"}},
			Ephemeral: true,
		})
	default:
		m.messages = append(m.messages, types.Message{
			Role:      types.RoleAssistant,
			Content:   []types.ContentBlock{{Type: types.BlockText, Text: "(steer dropped: queue full)"}},
			Ephemeral: true,
		})
	}
	return m, m.flush()
}

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
			m.messages = append(m.messages, types.Message{
				Role:      types.RoleAssistant,
				Content:   []types.ContentBlock{{Type: types.BlockText, Text: "(compacting, slash/shell commands not queued)"}},
				Ephemeral: true,
			})
			return m, m.flush()
		}
		m.queuedMidCompact = text
		m.messages = append(m.messages, types.Message{
			Role:      types.RoleAssistant,
			Content:   []types.ContentBlock{{Type: types.BlockText, Text: "(queued, runs after compact: " + text + ")"}},
			Ephemeral: true,
		})
		return m, m.flush()
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
		name := strings.SplitN(strings.TrimPrefix(text, "/"), " ", 2)[0]
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
		m.messages = append(m.messages, types.Message{
			Role:      types.RoleAssistant,
			Content:   []types.ContentBlock{{Type: types.BlockText, Text: "(/" + name + " unavailable while bee is running)"}},
			Ephemeral: true,
		})
		return m, m.flush()
	}
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	// record every accepted submission for ctrl+r reverse search.
	AppendHistory(text)

	// inline shell: ! follows the user's configured default (cfg.ShellBangSilent
	// — silent by default so quick lookups don't burn tokens). !! inverts the
	// default, giving a per-invocation escape hatch in either direction.
	if cmd, count, isInline := parseInlinePrefix(text); isInline {
		if m.eng == nil {
			return m, nil
		}
		silent := resolveBangSilent(m.shellBangSilent, count)
		res := runInlineShell(m.ctx, m.eng.Tools, cmd, silent)
		payload := formatInlineShell(cmd, res.Output, res.IsError)
		if silent {
			// local-only: record the styled shell exec, no engine turn.
			m.messages = append(m.messages, types.Message{
				Role:    types.RoleUser,
				Content: []types.ContentBlock{{Type: types.BlockText, Text: payload}},
			})
			return m, m.flush()
		}
		// non-silent: submit the shell record as the user turn so the LLM sees
		// the full cmd+output once and the scrollback shows a single styled card.
		return m.submit(payload)
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
	out := make([]types.Message, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Ephemeral {
			continue
		}
		out = append(out, msg)
	}
	return out
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
	// a fresh turn supersedes any stashed bail reason — a later /handoff must
	// not inherit a stall from an already-recovered failure.
	m.handoffStall = ""
	// fresh stream — drop any lingering flush state from a previous turn so
	// the next progressive flush starts clean.
	m.streamFlushed = ""
	m.streamFenceOpen = false
	m.pendingFlushedPrefix = ""
	m.partial = ""
	m.loaderFrame = 0
	// fresh token-stream loader: zero the live output counters and roll a new
	// procedural seed so this generation's particle layout is distinct.
	m.turnOutChars = 0
	if m.costs != nil {
		m.turnStartOutput = m.costs.Total().Output
	}
	m.loaderRate = 0
	m.loaderSampleChars = 0
	m.loaderSampleAt = time.Time{}
	m.loaderRateTokS = 0
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
		echo := types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(no engine: " + text + ")"}},
		}
		merged := append([]types.Message{}, m.messages...)
		merged = append(merged, echo)
		return m, tea.Batch(
			userFlush,
			func() tea.Msg {
				defer close(done)
				if prevDone != nil {
					<-prevDone
				}
				return turnDoneMsg{gen: gen, result: loop.RunResult{Messages: merged}}
			},
			loaderTickCmd(),
		)
	}

	parent := m.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	m.cancelRun = cancel
	eng := m.eng
	// prior turns for context. exclude the optimistic user msg we just appended
	// (history); prior keeps it (the full list mastermind rebuilds its result from).
	var history []types.Message
	if n := len(m.messages); n > 0 {
		history = nonEphemeral(m.messages[:n-1])
	}
	// mastermind tier: route the turn through a sub-agent hive instead of a
	// single engine Run. The glow tick keeps the rainbow input alive mid-turn.
	if m.role == "queen" {
		// the glow tick is already running (armed when the tier was enabled and
		// self-rearming across turns), so don't arm a second loop here.
		prior := append([]types.Message(nil), m.messages...)
		return m, tea.Batch(
			userFlush,
			m.runMastermind(ctx, gen, prevDone, done, content, history, prior),
			loaderTickCmd(),
		)
	}
	return m, tea.Batch(
		userFlush,
		func() tea.Msg {
			defer close(done)
			if prevDone != nil {
				<-prevDone // wait for a prior (possibly esc'd) run to fully return
			}
			// seed prior turns into the engine here, after the prior run released
			// it, so two runs never write InitialMessages concurrently.
			eng.InitialMessages = history
			res, err := eng.RunWithContentDisplay(ctx, content, display)
			return turnDoneMsg{gen: gen, result: res, err: err}
		},
		loaderTickCmd(),
	)
}
