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
	err := m.side().OpenSession(msg.ID)
	return m.failOn(err)
}

func (m Model) onSessionFork(msg SessionForkMsg) (tea.Model, tea.Cmd) {
	err := m.side().ForkSession(msg.FromID)
	return m.failOn(err)
}

func (m Model) onSessionClone(_ SessionCloneMsg) (tea.Model, tea.Cmd) {
	err := m.side().CloneSession()
	return m.failOn(err)
}

func (m Model) onOpenHive(_ openHiveMsg) (tea.Model, tea.Cmd) {
	if m.agentView != nil {
		m.agentView.Open()
		return m, m.agentView.Init()
	}
	return m, nil
}

func (m Model) onCloseAgentView() (tea.Model, tea.Cmd) {
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
