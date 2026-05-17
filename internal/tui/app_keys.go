package tui

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// classic readline chords must reach the textinput when the buffer has
	// content. Pane/quit shortcuts that overlap with editing chords
	// (ctrl+w word-back, ctrl+k kill-to-end, ctrl+h backspace, ctrl+t
	// transpose) only fire on an empty buffer. ctrl+d is handled globally
	// in Update (double-press to quit) so it never reaches handleKey.
	inputEmpty := m.input.Value() == ""
	keyStr := msg.String()
	// agent view: open with Left on empty input, close with Right/esc.
	// claim all keys while open so cursor moves never leak to the
	// textarea behind the overlay; delegate everything to AgentView.Update.
	if m.agentView != nil && m.agentView.IsOpen() {
		if keyStr == "right" {
			m.agentView.Close()
			return m, nil
		}
		var cmd tea.Cmd
		m.agentView, cmd = m.agentView.Update(msg)
		return m, cmd
	}
	if keyStr == "left" && inputEmpty && m.state == StateIdle {
		return m, func() tea.Msg { return openHiveMsg{} }
	}
	editingChord := keyStr == "ctrl+w" || keyStr == "ctrl+k" ||
		keyStr == "ctrl+h" || keyStr == "ctrl+t"
	switch {
	case key.Matches(msg, m.keys.Cancel):
		if m.state == StateStreaming {
			if m.cancelRun != nil {
				m.cancelRun()
				m.cancelRun = nil
			}
			m.state = StateIdle
		}
		return m, nil
	case key.Matches(msg, m.keys.FollowUp):
		return m.handleFollowUp()
	case key.Matches(msg, m.keys.ImagePaste):
		return m.handleImagePaste()
	case key.Matches(msg, m.keys.Submit):
		// state-dependent: idle = submit, streaming = steer. Slash commands
		// always route to handleSubmit so AllowDuringRun ones (/settings,
		// /effort, /model, …) work mid-stream instead of being captured as
		// steer text.
		if m.state == StateStreaming && !strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
			return m.handleSteer()
		}
		return m.handleSubmit()
	case key.Matches(msg, m.keys.ProviderPick):
		return m, func() tea.Msg { return openProviderMsg{} }
	case key.Matches(msg, m.keys.WorkspaceTog):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openWorkspaceMsg{} }
	case key.Matches(msg, m.keys.HiveOpen):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openHiveMsg{} }
	case key.Matches(msg, m.keys.SessionTree):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openTreeMsg{} }
	case key.Matches(msg, m.keys.CostOpen):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openCostMsg{} }
	case key.Matches(msg, m.keys.SlashPalette):
		if editingChord && !inputEmpty {
			break
		}
		return m, func() tea.Msg { return openPaletteMsg{} }
	case key.Matches(msg, m.keys.HistorySearch):
		// ctrl+r opens reverse history search with the current buffer as
		// initial filter — fzf-style.
		m.history.Show(m.input.Value())
		return m, nil
	case key.Matches(msg, m.keys.CavemanCycle):
		m.caveLvl = cycleCaveman(m.caveLvl)
		return m, nil
	case key.Matches(msg, m.keys.ThinkingCycle):
		m.thinking = cycleThinking(m.thinking)
		if m.eng != nil {
			m.eng.Cfg.Thinking = m.thinking
		}
		_ = PersistSetting("", "thinking", m.thinking)
		return m, nil
	case key.Matches(msg, m.keys.ModeCycle):
		prov := ""
		if m.eng != nil {
			prov = m.eng.Cfg.DefaultProvider
		}
		m.mode = cycleMode(m.mode, prov)
		if m.eng != nil {
			m.eng.Cfg.Mode = m.mode
		}
		return m, nil
	}
	// scroll keys are no-ops now — terminal handles native scroll back over
	// printed messages. PageUp/PageDown/Up/Down/ctrl+s used to drive the
	// viewport widget; with View() rendering only the live region they fall
	// through to the textarea (Up/Down move the cursor between input lines).
	// `?` toggles the help line when the input is empty — keeps the
	// chrome silent by default.
	if msg.String() == "?" && m.input.Value() == "" {
		m.showHelp = !m.showHelp
		return m, nil
	}
	// `/` while idle on an empty input opens the palette. The "/" itself
	// flows into the input bar so the user sees what they're typing;
	// subsequent chars also land in the input and the palette filter
	// mirrors via SetFilter (see KeyMsg branch in Update). Also recover
	// from StateError so a stream/provider failure doesn't lock out the
	// palette — user must still be able to /model, /login, /help.
	if msg.String() == "/" && m.input.Value() == "" && !m.palette.Active && (m.state == StateIdle || m.state == StateError) {
		if m.state == StateError {
			m.state = StateIdle
			m.lastErr = ""
		}
		m.palette.Show("")
		// fall through to let the textinput consume "/"
	}
	// `@` while idle opens the fuzzy file picker. Also recover from
	// StateError so the picker isn't gated behind a stale error.
	if msg.String() == "@" && !m.atpicker.Active && (m.state == StateIdle || m.state == StateError) {
		if m.state == StateError {
			m.state = StateIdle
			m.lastErr = ""
		}
		// insert the `@` literally so dismissing leaves it in place
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.atpicker = NewAtPicker(m.cwd)
		m.atpicker.SetWidth(m.width)
		m.atpicker.Active = true
		return m, tea.Batch(cmd)
	}
	// up/down cycle through past prompts (fish-style). Single-line buffers
	// route into cycling on up; once cycling, both arrows continue regardless
	// of line count (history entries may themselves be multi-line). Multi-
	// line buffers leave up/down alone so they navigate textarea rows.
	if (keyStr == "up" || keyStr == "down") && m.state == StateIdle {
		if m.cycleActive || (keyStr == "up" && m.input.LineCount() == 1) {
			if !m.cycleActive {
				m.cycleEntries = LoadHistory()
				m.cycleStash = m.input.Value()
				m.cycleIdx = -1
				m.cycleActive = true
			}
			if len(m.cycleEntries) == 0 {
				m.cycleActive = false
				return m, nil
			}
			if keyStr == "up" {
				if m.cycleIdx+1 < len(m.cycleEntries) {
					m.cycleIdx++
				}
				m.input.SetValue(m.cycleEntries[m.cycleIdx])
				m.input.CursorEnd()
				return m, nil
			}
			// down: walk back toward stash; past it ends the cycle.
			m.cycleIdx--
			if m.cycleIdx < 0 {
				m.cycleActive = false
				m.input.SetValue(m.cycleStash)
				m.input.CursorEnd()
				return m, nil
			}
			m.input.SetValue(m.cycleEntries[m.cycleIdx])
			m.input.CursorEnd()
			return m, nil
		}
	}
	// any other key while cycling resets the cycle — the landed-on entry
	// becomes the new base buffer for normal editing.
	if m.cycleActive {
		m.cycleActive = false
		m.cycleEntries = nil
		m.cycleStash = ""
		m.cycleIdx = -1
	}
	// Tab: try path completion on the cursor line. textarea exposes only
	// column-cursor, so we operate on the end of the current value when
	// the cursor is at end; otherwise fall through to default behavior.
	if msg.String() == "tab" && m.state == StateIdle {
		val := m.input.Value()
		// only auto-complete when buffer is a single line and cursor at end —
		// the 95% case for path completion. multi-line tabs fall through.
		if m.input.LineCount() == 1 {
			start := strings.LastIndexAny(val, " \t") + 1
			partial := val[start:]
			if partial != "" {
				dir := filepath.Dir(partial)
				if dir == "." || dir == "" {
					dir = m.cwd
				} else if !filepath.IsAbs(dir) {
					dir = filepath.Join(m.cwd, dir)
				}
				base := filepath.Base(partial)
				cands := CompletionCandidates(dir, base)
				if len(cands) > 0 {
					completion := LongestCommonPrefix(cands)
					if completion != base {
						add := completion[len(base):]
						m.input.SetValue(val + add)
						return m, nil
					}
				}
			}
		}
		// no path completion — let textinput accept the ghost suggestion.
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

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
		Role:    types.RoleAssistant,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "(queued: " + text + ")"}},
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
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(steer dropped: no channel)"}},
		})
		return m, m.flush()
	}
	select {
	case m.eng.SteerCh <- text:
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(steering: " + text + ")"}},
		})
	default:
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(steer dropped: queue full)"}},
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
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: "(/" + name + " unavailable while bee is running)"}},
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
		eng.InitialMessages = append([]types.Message{}, m.messages[:n-1]...)
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
