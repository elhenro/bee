package agents

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKey routes a key event. The input textarea is multi-line internally
// (shift+enter inserts a newline) but in practice we keep it single-line; the
// hjkl/arrow navigation only fires when the input is empty so typing is
// preserved.
func (m model) handleKey(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := km.String()
	val := m.input.Value()
	empty := strings.TrimSpace(val) == ""

	// global quit shortcuts
	switch s {
	case "ctrl+c":
		m.exitReq = true
		return m, tea.Quit
	}

	// list navigation — only when the input is empty, so the user can type freely
	if empty {
		switch s {
		case "j", "down":
			if m.sel < len(m.flat)-1 {
				m.sel++
			}
			return m, nil
		case "k", "up":
			if m.sel > 0 {
				m.sel--
			}
			return m, nil
		case "l", "right", "enter":
			// enter without input: open selected agent
			if s == "enter" && len(m.flat) == 0 {
				return m, nil
			}
			if r, ok := m.selected(); ok {
				m.attachID = r.Status.SessionID
				m.exitReq = true
				return m, tea.Quit
			}
			return m, nil
		case "h", "left":
			// left from overview is a no-op (overview IS the root)
			return m, nil
		case "m":
			if r, ok := m.selected(); ok {
				select {
				case m.retryCh <- r.Status.SessionID:
					m.flash("retry merge queued for " + r.Status.SessionID[:8])
				default:
					m.flash("merge queue full — try again")
				}
			}
			return m, nil
		case "q":
			m.exitReq = true
			return m, tea.Quit
		}
	}

	// enter with non-empty input — either slash command or spawn
	if s == "enter" && !empty {
		text := strings.TrimSpace(val)
		m.input.Reset()
		if strings.HasPrefix(text, "/") {
			return m.runSlash(text), nil
		}
		m.spawnAgent(text)
		return m, refreshCmd()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(km)
	return m, cmd
}

// runSlash handles /model, /provider, /quit, /help. Other slashes flash an
// "unknown" notice — full slash parity defers to the main TUI.
func (m model) runSlash(text string) model {
	parts := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(parts) == 0 {
		return m
	}
	switch parts[0] {
	case "model":
		if len(parts) < 2 {
			m.flash("usage: /model <model-name> (current: " + or(m.pendingModel, "default") + ")")
			return m
		}
		m.pendingModel = parts[1]
		m.flash("next spawn will use model " + m.pendingModel)
	case "provider":
		if len(parts) < 2 {
			m.flash("usage: /provider <name> (current: " + or(m.pendingProvider, "default") + ")")
			return m
		}
		m.pendingProvider = parts[1]
		m.flash("next spawn will use provider " + m.pendingProvider)
	case "quit", "exit":
		m.exitReq = true
	case "help":
		m.flash("keys: j/k nav · l/enter open · m merge · enter (with text) spawn · /model /provider")
	default:
		m.flash("unknown command /" + parts[0] + " (open full TUI with `bee` for all slash commands)")
	}
	return m
}

func or(a, b string) string {
	if a == "" {
		return b
	}
	return a
}
