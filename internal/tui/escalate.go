package tui

import (
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
	optionList
	options []string
}

// NewEscalateModel returns a fresh, inactive picker.
func NewEscalateModel(styles Styles) EscalateModel {
	return EscalateModel{optionList: optionList{styles: &styles}}
}

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

// Update handles picker key events. Returns the model + an optional cmd that
// publishes the choice. Caller forwards the cmd.
func (m EscalateModel) Update(msg tea.Msg) (EscalateModel, tea.Cmd) {
	if idx := m.key(msg, len(m.options)); idx >= 0 {
		return m.pick(idx)
	}
	if !m.Active {
		m.options = nil
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
	return m.render("", m.options, "↑↓ move · 1-9 pick · enter select · esc dismiss")
}
