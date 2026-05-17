package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) onOpenTree(_ openTreeMsg) (tea.Model, tea.Cmd) {
	if m.tree != nil {
		m.tree.LoadMessages(m.messages, m.currentLeafID())
		newT, cmd := m.tree.Update(ToggleSessionTreeMsg{})
		m.tree = newT
		return m, cmd
	}
	return m, nil
}

func (m Model) onOpenResume(_ openResumeMsg) (tea.Model, tea.Cmd) {
	if m.resume != nil {
		newR, cmd := m.resume.Update(ToggleResumePickerMsg{})
		m.resume = newR
		return m, cmd
	}
	return m, nil
}

func (m Model) onResumeSelect(msg ResumeSelectMsg) (tea.Model, tea.Cmd) {
	if err := m.side().OpenSession(msg.ID); err != nil {
		m.lastErr = err.Error()
		m.state = StateError
	}
	return m, nil
}

func (m Model) onSessionFork(msg SessionForkMsg) (tea.Model, tea.Cmd) {
	if err := m.side().ForkSession(msg.FromID); err != nil {
		m.lastErr = err.Error()
		m.state = StateError
	}
	return m, nil
}

func (m Model) onSessionClone(_ SessionCloneMsg) (tea.Model, tea.Cmd) {
	if err := m.side().CloneSession(); err != nil {
		m.lastErr = err.Error()
		m.state = StateError
	}
	return m, nil
}

func (m Model) onOpenHive(_ openHiveMsg) (tea.Model, tea.Cmd) {
	if m.agentView != nil {
		m.agentView.Open()
		return m, m.agentView.Init()
	}
	return m, nil
}

func (m Model) onCloseAgentView(_ CloseAgentViewMsg) (tea.Model, tea.Cmd) {
	if m.agentView != nil {
		m.agentView.Close()
	}
	return m, nil
}

func (m Model) onAttachSession(_ AttachSessionMsg) (tea.Model, tea.Cmd) {
	// attach defers to the side adapter; AgentView returns the id and the
	// outer app routes it to /attach. For now: just close the pane.
	if m.agentView != nil {
		m.agentView.Close()
	}
	return m, nil
}

func (m Model) onAgentTick(msg agentTickMsg) (tea.Model, tea.Cmd) {
	if m.agentView != nil && m.agentView.IsOpen() {
		var cmd tea.Cmd
		m.agentView, cmd = m.agentView.Update(msg)
		return m, cmd
	}
	return m, nil
}
