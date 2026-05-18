package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) onOpenCost(_ openCostMsg) (tea.Model, tea.Cmd) {
	if m.isLocalProvider() {
		m.lastErr = "cost monitor hidden for local provider"
		return m, nil
	}
	if m.costPane == nil {
		m.costPane = NewCostPane(m.costs)
	}
	newC, cmd := m.costPane.Update(ToggleCostPaneMsg{})
	m.costPane = newC
	return m, cmd
}

func (m Model) onOpenLogin(_ openLoginMsg) (tea.Model, tea.Cmd) {
	if m.loginPane == nil {
		m.loginPane = NewLoginPane(m.side())
	}
	newL, cmd := m.loginPane.Update(ToggleLoginPaneMsg{})
	m.loginPane = newL
	return m, cmd
}

func (m Model) onOpenEffort(_ openEffortMsg) (tea.Model, tea.Cmd) {
	if m.effortPane == nil {
		m.effortPane = NewEffortPane()
	}
	m.effortPane.Show(m.thinking)
	return m, nil
}

func (m Model) onOpenSettings(_ openSettingsMsg) (tea.Model, tea.Cmd) {
	if m.settingsPane == nil {
		m.settingsPane = NewSettingsPane()
	}
	m.settingsPane.Show(SettingsSnapshot{
		Verbose:         m.verbose,
		ShowThoughts:    m.showThoughts,
		ShowNudges:      m.showNudges,
		ShowRecap:       m.showRecap,
		Compact:         m.compact,
		ShowContextBar:  m.showContextBar,
		Highlight:       m.highlight,
		ShellBangSilent: m.shellBangSilent,
		ShowBee:         m.showBee,
		ShowContextPct:  m.showContextPct,
		ShowModel:       m.showModel,
		ShowCwd:         m.showCwd,
		ShowEffort:      m.showEffort,
		ShowTurnTimer:   m.showTurnTimer,
		ShowGitBranch:   m.showGitBranch,
		ShowTotalTokens: m.showTotalTokens,
		ShowBanner:      m.showBanner,
		ShowLoader:      m.showLoader,
	})
	return m, nil
}

func (m Model) onSettingsToggle(msg settingsToggleMsg) (tea.Model, tea.Cmd) {
	// each toggle applies live + persists; side handles all three.
	var err error
	switch msg.key {
	case "verbose":
		err = m.side().SetVerbose(msg.value)
	case "show_thoughts":
		err = m.side().SetShowThoughts(msg.value)
	case "show_nudges":
		err = m.side().SetShowNudges(msg.value)
	case "show_recap":
		err = m.side().SetShowRecap(msg.value)
	case "compact":
		err = m.side().SetCompact(msg.value)
	case "show_context_bar":
		err = m.side().SetShowContextBar(msg.value)
	case "highlight":
		err = m.side().SetHighlight(msg.value)
	case "shell_bang_silent":
		err = m.side().SetShellBangSilent(msg.value)
	case "show_bee":
		err = m.side().SetShowBee(msg.value)
	case "show_context_pct":
		err = m.side().SetShowContextPct(msg.value)
	case "show_model":
		err = m.side().SetShowModel(msg.value)
	case "show_cwd":
		err = m.side().SetShowCwd(msg.value)
	case "show_effort":
		err = m.side().SetShowEffort(msg.value)
	case "show_turn_timer":
		err = m.side().SetShowTurnTimer(msg.value)
	case "show_git_branch":
		err = m.side().SetShowGitBranch(msg.value)
	case "show_total_tokens":
		err = m.side().SetShowTotalTokens(msg.value)
	case "show_banner":
		err = m.side().SetShowBanner(msg.value)
	case "show_loader":
		err = m.side().SetShowLoader(msg.value)
	}
	if err != nil && m.state != StateStreaming {
		// don't kill an in-flight turn over a persist hiccup; surface the
		// error only when idle.
		m.lastErr = err.Error()
		m.state = StateError
	}
	return m, nil
}

func (m Model) onOpenTools(_ openToolsMsg) (tea.Model, tea.Cmd) {
	if m.toolsPane == nil {
		m.toolsPane = NewToolsPane()
	}
	m.toolsPane.Show(m.side().ListTools())
	return m, nil
}

func (m Model) onToolsToggle(msg toolsToggleMsg) (tea.Model, tea.Cmd) {
	if err := m.side().SetToolDisabled(msg.name, msg.disabled); err != nil && m.state != StateStreaming {
		m.lastErr = err.Error()
		m.state = StateError
	}
	return m, nil
}

func (m Model) onLoginActionDone(msg loginActionDoneMsg) (tea.Model, tea.Cmd) {
	// async login/logout finished — let the pane absorb it even if a
	// key in the meantime closed the pane (so busy flag clears).
	if m.loginPane != nil {
		newL, cmd := m.loginPane.Update(msg)
		m.loginPane = newL
		return m, cmd
	}
	return m, nil
}
