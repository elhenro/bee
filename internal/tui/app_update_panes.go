package tui

import (
	"cmp"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/commands"
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
		m.lastErr = "cost monitor hidden for local provider"
		return m, nil
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
	m.settingsPane.Show(SettingsSnapshot{
		Verbose:         m.verbose,
		ShowThoughts:    m.showThoughts,
		ShowNudges:      m.showNudges,
		ShowRecap:       m.showRecap,
		Compact:         m.compact,
		ShowContextBar:  m.showContextBar,
		Highlight:       m.highlight,
		ShowCallDetail:  m.showCallDetail,
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
		ShowLoaderIn:    m.showLoaderIn,
		ShowLoaderOut:   m.showLoaderOut,
		ShowLoaderRate:  m.showLoaderRate,
		OuterPadLR:      m.outerPadLR,
	})
	return m, nil
}

// settingSetters maps a settings-pane toggle key to the Side setter that
// applies it live and persists it.
var settingSetters = map[string]func(commands.Side, bool) error{
	"verbose":           commands.Side.SetVerbose,
	"show_thoughts":     commands.Side.SetShowThoughts,
	"show_nudges":       commands.Side.SetShowNudges,
	"show_recap":        commands.Side.SetShowRecap,
	"compact":           commands.Side.SetCompact,
	"show_context_bar":  commands.Side.SetShowContextBar,
	"highlight":         commands.Side.SetHighlight,
	"show_call_detail":  commands.Side.SetShowCallDetail,
	"shell_bang_silent": commands.Side.SetShellBangSilent,
	"show_bee":          commands.Side.SetShowBee,
	"show_context_pct":  commands.Side.SetShowContextPct,
	"show_model":        commands.Side.SetShowModel,
	"show_cwd":          commands.Side.SetShowCwd,
	"show_effort":       commands.Side.SetShowEffort,
	"show_turn_timer":   commands.Side.SetShowTurnTimer,
	"show_git_branch":   commands.Side.SetShowGitBranch,
	"show_total_tokens": commands.Side.SetShowTotalTokens,
	"show_banner":       commands.Side.SetShowBanner,
	"show_loader":       commands.Side.SetShowLoader,
	"show_loader_in":    commands.Side.SetShowLoaderIn,
	"show_loader_out":   commands.Side.SetShowLoaderOut,
	"show_loader_rate":  commands.Side.SetShowLoaderRate,
}

// failQuietly surfaces a persist error unless a turn is streaming: don't kill
// an in-flight turn over a persist hiccup.
func (m Model) failQuietly(err error) Model {
	if err != nil && m.state != StateStreaming {
		m.lastErr = err.Error()
		m.state = StateError
	}
	return m
}

func (m Model) onSettingsToggle(msg settingsToggleMsg) (tea.Model, tea.Cmd) {
	var err error
	if set := settingSetters[msg.key]; set != nil {
		err = set(m.side(), msg.value)
	}
	return m.failQuietly(err), nil
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
	return m.failQuietly(err), nil
}

func (m Model) onOpenTools(_ openToolsMsg) (tea.Model, tea.Cmd) {
	if m.toolsPane == nil {
		m.toolsPane = NewToolsPane()
	}
	m.toolsPane.Show(m.side().ListTools())
	return m, nil
}

func (m Model) onToolsToggle(msg toolsToggleMsg) (tea.Model, tea.Cmd) {
	return m.failQuietly(m.side().SetToolDisabled(msg.name, msg.disabled)), nil
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
