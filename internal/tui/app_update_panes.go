package tui

import (
	"cmp"

	tea "github.com/charmbracelet/bubbletea"
)

// togglePane sends the toggle message to the pane at *p, creating it on first use.
func togglePane[P keyPane[P]](p *P, create func() P, toggle tea.Msg) tea.Cmd {
	var unset P
	if *p == unset {
		*p = create()
	}
	var cmd tea.Cmd
	*p, cmd = (*p).Update(toggle)
	return cmd
}

func (m Model) onOpenCost(_ openCostMsg) (tea.Model, tea.Cmd) {
	if m.isLocalProvider() {
		return m.fail("cost monitor hidden for local provider")
	}
	return m, togglePane(&m.costPane, func() *CostPane { return NewCostPane(m.costs) }, ToggleCostPaneMsg{})
}

// onOpenUsage toggles the historical usage pane. Unlike the cost monitor it is
// not gated on the active provider: past usage may include non-local providers
// even when the current one is local.
func (m Model) onOpenUsage(_ openUsageMsg) (tea.Model, tea.Cmd) {
	return m, togglePane(&m.usagePane, func() *UsagePane { return NewUsagePane(m.costs) }, ToggleUsagePaneMsg{})
}

func (m Model) onOpenLogin(_ openLoginMsg) (tea.Model, tea.Cmd) {
	return m, togglePane(&m.loginPane, func() *LoginPane { return NewLoginPane(m.side()) }, ToggleLoginPaneMsg{})
}

func (m Model) onOpenRole(_ openRoleMsg) (tea.Model, tea.Cmd) {
	if m.rolePane == nil {
		m.rolePane = NewRolePane()
	}
	m.rolePane.Show(cmp.Or(m.role, "worker"))
	return m, nil
}

func (m Model) onOpenEffort(_ openEffortMsg) (tea.Model, tea.Cmd) {
	if m.effortPane == nil {
		m.effortPane = NewEffortPane()
	}
	m.effortPane.Show(cmp.Or(m.thinking, "auto"))
	return m, nil
}

func (m Model) onOpenSettings(_ openSettingsMsg) (tea.Model, tea.Cmd) {
	if m.settingsPane == nil {
		m.settingsPane = NewSettingsPane()
	}
	on := make(map[string]bool, len(boolSettings))
	for _, f := range boolSettings {
		on[f.key] = f.value(&m)
	}
	m.settingsPane.Show(SettingsSnapshot{On: on, OuterPadLR: m.outerPadLR})
	return m, nil
}

func (m Model) onSettingsToggle(msg settingsToggleMsg) (tea.Model, tea.Cmd) {
	var err error
	if f, ok := flagByKey(msg.key); ok {
		err = (&tuiSide{m: &m}).setFlag(f, msg.value)
	}
	return m.failOn(err)
}

// onSettingsSetInt handles the int-magnitude row cycle (only outer_pad_lr
// today). Mirrors onSettingsToggle's apply+persist contract, but updates the
// live field on the Model in addition to delegating to Side, so the next
// View() picks the new padding up without restart.
func (m Model) onSettingsSetInt(msg settingsSetIntMsg) (tea.Model, tea.Cmd) {
	var err error
	if msg.key == "outer_pad_lr" {
		m.outerPadLR = msg.value
		err = m.side().SetOuterPadLR(msg.value)
	}
	return m.failOn(err)
}

func (m Model) onOpenTools(_ openToolsMsg) (tea.Model, tea.Cmd) {
	if m.toolsPane == nil {
		m.toolsPane = NewToolsPane()
	}
	m.toolsPane.Show(m.side().ListTools())
	return m, nil
}

func (m Model) onToolsToggle(msg toolsToggleMsg) (tea.Model, tea.Cmd) {
	return m.failOn(m.side().SetToolDisabled(msg.name, msg.disabled))
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
