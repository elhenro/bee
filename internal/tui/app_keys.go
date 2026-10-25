package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// recoverError clears a stale error so a gated input (palette, @ picker)
// isn't locked behind it.
func (m *Model) recoverError() {
	if m.state == StateError {
		m.state = StateIdle
		m.lastErr = ""
	}
}

// resetCycle drops the up/down prompt-cycling session.
func (m *Model) resetCycle() {
	m.cycleActive, m.cycleEntries, m.cycleStash, m.cycleIdx = false, nil, "", -1
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// classic readline chords must reach the textinput when the buffer has
	// content. Pane/quit shortcuts that overlap with editing chords
	// (ctrl+w word-back, ctrl+k kill-to-end, ctrl+h backspace, ctrl+t
	// transpose) only fire on an empty buffer. ctrl+d is handled globally
	// in Update (double-press to quit) so it never reaches handleKey.
	inputEmpty := m.input.Value() == ""
	keyStr := msg.String()
	// bracketed paste: a dragged image file path arrives as one atomic paste.
	// stage it as an image and insert a clean label instead of the raw path.
	if msg.Paste && (m.state == StateIdle || m.state == StateError) {
		return m.handlePaste(msg)
	}
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
	// left on empty input goes back to the `bee agents` overview when this
	// TUI was launched from it (BEE_FROM_AGENTS=1 set by cmd/bee/agents.go);
	// quitting the program lets the agents loop redraw the overview.
	if keyStr == "left" && inputEmpty && os.Getenv("BEE_FROM_AGENTS") == "1" {
		if m.cancelRun != nil {
			m.cancelRun()
			m.cancelRun = nil
		}
		return m, tea.Quit
	}
	if keyStr == "left" && inputEmpty && m.state == StateIdle {
		return m, func() tea.Msg { return openHiveMsg{} }
	}
	editingChord := keyStr == "ctrl+w" || keyStr == "ctrl+k" ||
		keyStr == "ctrl+h" || keyStr == "ctrl+t"
	switch {
	case key.Matches(msg, m.keys.Cancel):
		if m.state == StateStreaming {
			m.cancelTurn()
			m.interrupted = true
		}
		return m, nil
	case key.Matches(msg, m.keys.FollowUp):
		return m.handleFollowUp()
	case key.Matches(msg, m.keys.ImagePaste):
		return m.handleImagePaste()
	case key.Matches(msg, m.keys.ExpandOutput):
		// parked external job (Ctrl-Z'd /edit etc.) takes priority — resume
		// it where it left off. otherwise toggle last tool output detail.
		if m.suspendedJob != nil {
			return m, m.resumeJob()
		}
		return m.toggleLastToolOutput()
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
	case key.Matches(msg, m.keys.HiveOpen, m.keys.SessionTree, m.keys.CostOpen, m.keys.SlashPalette):
		if editingChord && !inputEmpty {
			break
		}
		for _, p := range []struct {
			chord key.Binding
			open  tea.Msg
		}{
			{m.keys.HiveOpen, openHiveMsg{}}, {m.keys.SessionTree, openTreeMsg{}},
			{m.keys.CostOpen, openCostMsg{}}, {m.keys.SlashPalette, openPaletteMsg{}},
		} {
			if key.Matches(msg, p.chord) {
				return m, func() tea.Msg { return p.open }
			}
		}
	case key.Matches(msg, m.keys.HistorySearch):
		// ctrl+r opens reverse history search with the current buffer as
		// initial filter — fzf-style, rendered inline above the input.
		m.history.SetWidth(m.width)
		m.history.Show(m.input.Value())
		return m, nil
	case key.Matches(msg, m.keys.CavemanCycle):
		m.caveLvl = cycleCaveman(m.caveLvl)
		return m, nil
	case key.Matches(msg, m.keys.RoleCycle):
		prev := m.role
		m.role = cycleRole(m.role)
		m.thinking = roleThinking(m.role)
		m.mirrorToEngine()
		_ = PersistSetting("", "role", m.role)
		// entering queen: arm exactly one rainbow glow loop + a one-shot warning.
		// substantial tasks spawn a planning+review hive; small/clear ones are
		// triaged to a single pass.
		if m.role == "queen" && prev != "queen" {
			m = m.withWarn("queen: big tasks spawn a planning+review hive; small ones run single-pass — higher quality, more tokens")
			return m, tea.Batch(glowTickCmd(), warningFadeCmd())
		}
		return m, nil
	case key.Matches(msg, m.keys.YoloToggle):
		m.yolo = !m.yolo
		m.mirrorToEngine()
		_ = PersistSetting("", "yolo", m.yolo)
		// loud one-shot warning when arming; disarming is quiet.
		if m.yolo {
			m = m.withWarn("yolo ON — dangerous shell auto-approves, no prompt")
			return m, warningFadeCmd()
		}
		return m, nil
	}
	// scroll keys are no-ops now — terminal handles native scroll back over
	// printed messages. PageUp/PageDown/Up/Down/ctrl+s used to drive the
	// viewport widget; with View() rendering only the live region they fall
	// through to the textarea (Up/Down move the cursor between input lines).
	// `?` toggles the help line when the input is empty — keeps the
	// chrome silent by default.
	if keyStr == "?" && inputEmpty {
		m.showHelp = !m.showHelp
		return m, nil
	}
	// `/` while idle on an empty input opens the palette. The "/" itself
	// flows into the input bar so the user sees what they're typing;
	// subsequent chars also land in the input and the palette filter
	// mirrors via SetFilter (see KeyMsg branch in Update). Also recover
	// from StateError so a stream/provider failure doesn't lock out the
	// palette — user must still be able to /model, /login, /help.
	// Also open mid-stream: AllowDuringRun commands (/settings, /model, …)
	// are dispatchable then, so they need their autocomplete too.
	if keyStr == "/" && inputEmpty && !m.palette.Active {
		m.recoverError()
		m.showPalette("")
		// fall through to let the textinput consume "/"
	}
	// `@` while idle opens the fuzzy file picker. Also recover from
	// StateError so the picker isn't gated behind a stale error.
	if keyStr == "@" && !m.atpicker.Active && (m.state == StateIdle || m.state == StateError) {
		m.recoverError()
		// insert the `@` literally so dismissing leaves it in place
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.atpicker = NewAtPicker(m.cwd)
		m.atpicker.SetWidth(m.width)
		m.atpicker.Active = true
		return m, cmd
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
				m.cycleIdx = min(m.cycleIdx+1, len(m.cycleEntries)-1)
			} else {
				m.cycleIdx-- // down walks back toward the stash; past it ends the cycle
			}
			if m.cycleIdx < 0 {
				m.cycleActive = false
				m.input.SetValue(m.cycleStash)
			} else {
				m.input.SetValue(m.cycleEntries[m.cycleIdx])
			}
			m.input.CursorEnd()
			return m, nil
		}
	}
	// any other key while cycling resets the cycle — the landed-on entry
	// becomes the new base buffer for normal editing.
	if m.cycleActive {
		m.resetCycle()
	}
	// Tab: try path completion on the cursor line. textarea exposes only
	// column-cursor, so we operate on the end of the current value when
	// the cursor is at end; otherwise fall through to default behavior.
	// only auto-complete when buffer is a single line and cursor at end —
	// the 95% case for path completion. multi-line tabs fall through.
	if keyStr == "tab" && m.state == StateIdle && m.input.LineCount() == 1 {
		if completed, ok := completePath(m.cwd, m.input.Value()); ok {
			m.input.SetValue(completed)
			return m, nil
		}
		// no path completion — let textinput accept the ghost suggestion.
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
