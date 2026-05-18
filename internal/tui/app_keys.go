package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
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
		// initial filter — fzf-style, rendered inline above the input.
		m.history.SetWidth(m.width)
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
