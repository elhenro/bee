package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/update"
)

// onUpdateAvailable surfaces the four-button modal. Skips if the user already
// picked "later" this session, or if an install is already in flight.
func (m Model) onUpdateAvailable(msg updateAvailableMsg) (tea.Model, tea.Cmd) {
	if m.updateSeenSession || m.updateApplying {
		return m, nil
	}
	m.updatePrompt.Show(msg.Info)
	return m, nil
}

// onUpdateDecision routes the modal's choice. Persistence happens here so
// the modal stays a thin view.
func (m Model) onUpdateDecision(msg updateDecisionMsg) (tea.Model, tea.Cmd) {
	switch msg.Decision {
	case UpdateLater:
		m.updateSeenSession = true
		return m, nil
	case UpdateNow:
		return m.kickApply(msg.Info)
	case UpdateAlways:
		var warn string
		if err := PersistSetting("", "update_check", "auto"); err != nil {
			warn = "persist update_check=auto: " + err.Error()
		}
		if m.eng != nil {
			m.eng.Cfg.UpdateCheck = "auto"
		}
		nm, cmd := m.kickApply(msg.Info)
		if warn != "" {
			nm = nm.withWarn(warn)
		}
		return nm, cmd
	case UpdateNeverAsk:
		if err := PersistSetting("", "update_check", "off"); err != nil {
			return m.withWarn("persist update_check=off: " + err.Error()), warningFadeCmd()
		}
		if m.eng != nil {
			m.eng.Cfg.UpdateCheck = "off"
		}
		m.updateSeenSession = true
	}
	return m, nil
}

// kickApply runs the installer subprocess from a goroutine so the TUI stays
// responsive. The result lands in onUpdateApplied via tea.Cmd.
func (m Model) kickApply(info update.Info) (Model, tea.Cmd) {
	if m.updateApplying {
		return m, nil
	}
	m.updateApplying = true
	m = m.withWarn("updating bee… (" + shortSHA(info.LatestSHA) + ")")
	applyCmd := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		out, err := update.Apply(ctx)
		return updateAppliedMsg{output: string(out), err: err}
	}
	return m, tea.Batch(applyCmd, warningFadeCmd())
}

func (m Model) onUpdateApplied(msg updateAppliedMsg) (tea.Model, tea.Cmd) {
	m.updateApplying = false
	if msg.err != nil {
		hint := summarizeApplyOutput(msg.output)
		if hint == "" {
			hint = msg.err.Error()
		}
		return m.withWarn("update failed: " + hint + " — try: " + update.InstallCommand()), warningFadeCmd()
	}
	return m.withWarn("✓ bee updated — restart to use new version"), warningFadeCmd()
}

// withWarn stamps a transient notice that fades after warningTTL. Returns a
// copy so callers using value-receiver handlers can chain in `return`.
func (m Model) withWarn(text string) Model {
	m.warning = text
	m.warningExpires = time.Now().Add(warningTTL)
	return m
}

func shortSHA(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}
