package tui

import "strings"

// the per-item renderers behind statusRegistry

// drawStatusModel renders the provider-qualified model label.
func drawStatusModel(m Model) string {
	if !m.showModel {
		return ""
	}
	return m.styles.Key.Render(m.displayModel())
}

// the model without its provider or route prefix
func drawStatusModelShort(m Model) string {
	if !m.showModel {
		return ""
	}
	name := m.model
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return m.styles.Key.Render(name)
}

// drawStatusCwd renders the $HOME-shortened working directory.
func drawStatusCwd(m Model) string {
	if !m.showCwd {
		return ""
	}
	return m.styles.Dim.Render(shortCwd(prettyCwd(m.cwd), 32))
}

// drawStatusGitBranch renders the current branch, or "" outside a repo.
func drawStatusGitBranch(m Model) string {
	if !m.showGitBranch {
		return ""
	}
	if br := gitBranch(m.cwd); br != "" {
		return m.styles.Dim.Render(glyphBranch + " " + br)
	}
	return ""
}

// drawStatusSessionTitle renders a short session label so the line can name
// the session it belongs to. Empty when no rollout is attached.
func drawStatusSessionTitle(m Model) string {
	if m.eng == nil || m.eng.Sessions == nil {
		return ""
	}
	id := m.eng.Sessions.ID()
	if id == "" {
		return ""
	}
	if len(id) > 8 {
		id = id[:8]
	}
	return m.styles.Dim.Render(id)
}

// drawStatusCost renders the running spend badge, suppressed for local
// providers and while nothing has been spent.
func drawStatusCost(m Model) string {
	if m.costs == nil || m.isLocalProvider() {
		return ""
	}
	tot := m.costs.Total()
	if tot.USD <= 0 {
		return ""
	}
	return m.renderCostBadge(tot.USD)
}

// drawStatusTotalTokens renders the session's cumulative token count.
func drawStatusTotalTokens(m Model) string {
	if !m.showTotalTokens || m.costs == nil {
		return ""
	}
	tot := m.costs.Total()
	if n := tot.Input + tot.Output; n > 0 {
		return m.styles.Dim.Render(tokensHuman(n) + " tok")
	}
	return ""
}

// drawStatusGoal renders the compact goal-progress indicator when a goal is
// active.
func drawStatusGoal(m Model) string {
	if gl := m.goalStatusLine(); gl != "" {
		return m.styles.Dim.Render("◎ " + gl)
	}
	return ""
}

// contextual key hint, far right: what's useful now, not a manual. hidden
// mid-turn (the busy row has it) and while a picker owns the keys
func drawStatusHints(m Model) string {
	if m.state == StateStreaming || m.showHelp || m.pickerOpen() {
		return ""
	}
	if m.input.Value() != "" {
		return m.hints([][2]string{{"enter", "send"}, {"shift+↵", "newline"}})
	}
	return m.hints([][2]string{{"/", "commands"}, {"@", "files"}, {"?", "help"}})
}

// ctrl+o offer after a tool card that cut rows: gone once you type, send or
// expand, and shed after the always-on hints
func drawStatusExpand(m Model) string {
	if !m.lastToolClipped || m.input.Value() != "" || m.state == StateStreaming || m.showHelp || m.pickerOpen() {
		return ""
	}
	return m.hints([][2]string{{"ctrl+o", "expand"}})
}

// drawStatusRole renders the active-role chip (gated by the effort toggle).
func drawStatusRole(m Model) string {
	if !m.showEffort {
		return ""
	}
	return m.renderRoleBadge()
}

// drawStatusEffort renders the reasoning-budget chip (gated by the effort
// toggle so role + effort share one chrome slot).
func drawStatusEffort(m Model) string {
	if !m.showEffort {
		return ""
	}
	return m.renderEffortBadge()
}
