package tui

import "strings"

// statusAlign selects the edge an item hugs. Items render left-to-right in
// the configured order; the first right-aligned item opens the gap that
// pushes it (and everything after it) to the terminal's right edge. Because
// the split point is the first right-aligned id, the visible sequence always
// matches the configured order — right alignment is a spacing effect, not a
// reordering.
type statusAlign int

const (
	alignLeft statusAlign = iota
	alignRight
)

// statusItem maps a stable id to its renderer and default edge. A renderer
// returns "" when the item has nothing to show (its toggle is off, or it
// carries no data); empty items drop out silently without leaving a gap.
type statusItem struct {
	id    string
	align statusAlign
	draw  func(m Model) string
}

// statusRegistry is the id → item table and the single source of truth for
// which status-line items exist. Adding an item here is all it takes to make
// it selectable via tui.status_line. Later cards layer on top of it (machine
// mode, narrow-terminal fitting, alias resolution) without changing callers.
var statusRegistry = map[string]statusItem{
	"context":       {id: "context", align: alignLeft, draw: func(m Model) string { return m.renderContextHex() }},
	"model":         {id: "model", align: alignLeft, draw: drawStatusModel},
	"cwd":           {id: "cwd", align: alignLeft, draw: drawStatusCwd},
	"git_branch":    {id: "git_branch", align: alignLeft, draw: drawStatusGitBranch},
	"session_title": {id: "session_title", align: alignLeft, draw: drawStatusSessionTitle},
	"cost":          {id: "cost", align: alignLeft, draw: drawStatusCost},
	"total_tokens":  {id: "total_tokens", align: alignLeft, draw: drawStatusTotalTokens},
	"goal":          {id: "goal", align: alignLeft, draw: drawStatusGoal},
	"turn_timer":    {id: "turn_timer", align: alignRight, draw: func(m Model) string { return m.renderTurnTimer() }},
	"yolo":          {id: "yolo", align: alignRight, draw: func(m Model) string { return m.renderYoloBadge() }},
	"role":          {id: "role", align: alignRight, draw: drawStatusRole},
	"effort":        {id: "effort", align: alignRight, draw: drawStatusEffort},
}

// defaultStatusLineIDs reproduces bee's historical top bar: identity/context
// on the left, safety + reasoning chips pinned right. Used when the config
// key is absent (nil), as opposed to an explicit empty list which hides the
// line entirely.
func defaultStatusLineIDs() []string {
	return []string{
		"context", "model", "cwd", "git_branch", "cost", "total_tokens", "goal",
		"turn_timer", "yolo", "role", "effort",
	}
}

// resolveStatusLine turns a configured id list into the ordered ids to draw.
// A nil list (key absent) yields the defaults; an explicit empty list yields
// nothing (the caller hides the line). Unknown ids are skipped and duplicates
// keep the first occurrence. It never errors — a damaged list degrades to a
// shorter, possibly empty, line rather than blocking startup.
func resolveStatusLine(ids []string) []string {
	if ids == nil {
		return defaultStatusLineIDs()
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		if _, ok := statusRegistry[id]; !ok {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// statusLineAllOff reports whether every user-facing chrome toggle is off.
// On the default (unconfigured) path this collapses the whole row to nothing,
// preserving the historical "flip all toggles off -> no status line" behavior.
// An explicit config list bypasses it so a user can pin any item they like.
func (m Model) statusLineAllOff() bool {
	return !m.showBee && !m.showContextPct && !m.showModel && !m.showCwd &&
		!m.showEffort && !m.showTurnTimer && !m.showGitBranch && !m.showTotalTokens
}

// renderStatusLine composes the configured items into one width-fitting row.
// Empty items drop out. The first right-aligned item opens the gap that
// right-aligns the remainder, so the visible order always equals the
// configured order. Width is measured with lipglossWidth (ANSI-aware) and
// overflow is clipped with truncateVisible so styling never miscounts cells.
func (m Model) renderStatusLine() string {
	if m.statusLine == nil && m.statusLineAllOff() {
		return ""
	}
	ids := resolveStatusLine(m.statusLine)
	if len(ids) == 0 {
		return ""
	}
	var left, right []string
	rightOpen := false
	for _, id := range ids {
		it := statusRegistry[id]
		if it.align == alignRight {
			rightOpen = true
		}
		s := it.draw(m)
		if s == "" {
			continue
		}
		if rightOpen {
			right = append(right, s)
		} else {
			left = append(left, s)
		}
	}
	l := strings.Join(left, "  ")
	r := strings.Join(right, "  ")
	switch {
	case l == "" && r == "":
		return ""
	case m.width <= 0:
		if l == "" {
			return r
		}
		if r == "" {
			return l
		}
		return l + "  " + r
	}
	pad := m.width - lipglossWidth(l) - lipglossWidth(r)
	if pad < 1 {
		pad = 1
	}
	line := l + strings.Repeat(" ", pad) + r
	if lipglossWidth(line) > m.width {
		line = truncateVisible(line, m.width)
	}
	return line
}

// drawStatusModel renders the provider-qualified model label.
func drawStatusModel(m Model) string {
	if !m.showModel {
		return ""
	}
	return m.styles.Dim.Render(m.displayModel())
}

// drawStatusCwd renders the $HOME-shortened working directory.
func drawStatusCwd(m Model) string {
	if !m.showCwd {
		return ""
	}
	return m.styles.Dim.Render(prettyCwd(m.cwd))
}

// drawStatusGitBranch renders the current branch, or "" outside a repo.
func drawStatusGitBranch(m Model) string {
	if !m.showGitBranch {
		return ""
	}
	if br := gitBranch(m.cwd); br != "" {
		return m.styles.Dim.Render("⎇ " + br)
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
		return m.styles.Dim.Render("Σ" + tokensHuman(n))
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
