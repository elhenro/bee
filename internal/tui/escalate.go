package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// EscalateChoiceMsg is published when the user picks an escalate option. The
// parent submits Text as the next user turn.
type EscalateChoiceMsg struct {
	Text string
}

// EscalateModel is the interactive option picker shown after the model calls
// the escalate tool with discrete options. The yellow escalate card (reason +
// badge) already sits in scrollback; this picker renders only the selectable
// option list beneath it, tied in with the same mustard warn rail. Inactive =
// renders nothing.
type EscalateModel struct {
	styles  Styles
	Active  bool
	options []string
	focus   int
	width   int
}

// NewEscalateModel returns a fresh, inactive picker.
func NewEscalateModel(styles Styles) EscalateModel {
	return EscalateModel{styles: styles}
}

// SetWidth records the terminal width so View can wrap option rows.
func (m *EscalateModel) SetWidth(w int) { m.width = w }

// Show opens the picker for the given options. No-op when opts is empty so a
// reason-only escalate stays a plain card.
func (m *EscalateModel) Show(opts []string) {
	if len(opts) == 0 {
		return
	}
	m.options = opts
	m.Active = true
	m.focus = 0
}

// Hide closes the picker without publishing a choice.
func (m *EscalateModel) Hide() {
	m.Active = false
	m.options = nil
}

// Update handles picker key events. Returns the model + an optional cmd that
// publishes the choice. Caller forwards the cmd.
func (m EscalateModel) Update(msg tea.Msg) (EscalateModel, tea.Cmd) {
	if !m.Active {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "up", "k", "shift+tab":
		m.focus = (m.focus + len(m.options) - 1) % len(m.options)
	case "down", "j", "tab":
		m.focus = (m.focus + 1) % len(m.options)
	case "esc":
		m.Hide()
		return m, nil
	case "enter":
		return m.pick(m.focus)
	default:
		if len(km.String()) == 1 {
			if c := km.String()[0]; c >= '1' && c <= '9' {
				if idx := int(c - '1'); idx < len(m.options) {
					return m.pick(idx)
				}
			}
		}
	}
	return m, nil
}

func (m EscalateModel) pick(idx int) (EscalateModel, tea.Cmd) {
	text := m.options[idx]
	m.Active = false
	m.options = nil
	return m, func() tea.Msg { return EscalateChoiceMsg{Text: text} }
}

// View renders the option list. The parent appends it under the live region,
// directly below the escalate card already in scrollback.
func (m EscalateModel) View() string {
	if !m.Active {
		return ""
	}
	rail := m.styles.WarnRail.Render("▎")
	dim := m.styles.Dim
	width := m.width - 6
	if width < 20 {
		width = 20
	}
	var lines []string
	for i, opt := range m.options {
		label := pad(i+1) + opt
		wrapped := wrapHanging(label, width)
		for j, wl := range wrapped {
			switch {
			case j > 0:
				lines = append(lines, rail+"      "+dim.Render(wl))
			case i == m.focus:
				lines = append(lines, rail+" "+m.styles.ButtonHot.Render("›"+wl))
			default:
				lines = append(lines, rail+"   "+wl)
			}
		}
	}
	lines = append(lines, rail+" "+dim.Render("↑↓ move · 1-9 pick · enter select · esc dismiss"))
	return strings.Join(lines, "\n")
}
