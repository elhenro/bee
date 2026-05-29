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
		m.state = StateIdle
		m.lastErr = ""
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
	m.state = StateStreaming
	// fresh stream — drop any lingering flush state from a previous turn so
	// the next progressive flush starts clean.
	m.streamFlushed = ""
	m.streamFenceOpen = false
	m.pendingFlushedPrefix = ""
	m.partial = ""
	m.loaderFrame = 0
	// stamp turn start; clear last duration so the timer chip switches from
	// "final" to "live" mode immediately, no stale final reading lingering.
	m.turnStartedAt = time.Now()
	m.lastTurnDuration = 0
	// invalidate any pending post-turn recap tick from the previous turn.
	m.recapGen++

	// build content blocks: text first, then a pending image if staged.
	content := []types.ContentBlock{{Type: types.BlockText, Text: text}}
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
	})
	userFlush := m.flush()

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
			func() tea.Msg { return turnDoneMsg{result: loop.RunResult{Messages: merged}} },
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
	// seed prior turns into engine so the model retains context across
	// submits. exclude the optimistic user msg we just appended — engine
	// adds its own properly-IDed copy.
	if n := len(m.messages); n > 0 {
		eng.InitialMessages = nonEphemeral(m.messages[:n-1])
	} else {
		eng.InitialMessages = nil
	}
	return m, tea.Batch(
		userFlush,
		func() tea.Msg {
			res, err := eng.RunWithContent(ctx, content)
			return turnDoneMsg{result: res, err: err}
		},
		loaderTickCmd(),
	)
}
