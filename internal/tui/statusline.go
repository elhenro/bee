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
	"role":          {id: "role", align: alignLeft, draw: drawStatusRole},
	"effort":        {id: "effort", align: alignLeft, draw: drawStatusEffort},
	"yolo":          {id: "yolo", align: alignLeft, draw: func(m Model) string { return m.renderYoloBadge() }},
	"goal":          {id: "goal", align: alignLeft, draw: drawStatusGoal},
	"session_title": {id: "session_title", align: alignLeft, draw: drawStatusSessionTitle},
	"cwd":           {id: "cwd", align: alignRight, draw: drawStatusCwd},
	"git_branch":    {id: "git_branch", align: alignRight, draw: drawStatusGitBranch},
	"cost":          {id: "cost", align: alignRight, draw: drawStatusCost},
	"total_tokens":  {id: "total_tokens", align: alignRight, draw: drawStatusTotalTokens},
	"turn_timer":    {id: "turn_timer", align: alignRight, draw: func(m Model) string { return m.renderTurnTimer() }},
	"expand":        {id: "expand", align: alignRight, draw: drawStatusExpand},
	"hints":         {id: "hints", align: alignRight, draw: drawStatusHints},
}

// default footer: what you're talking to on the left, where and how much on
// the right, a contextual key hint last. used when the config key is absent
// (nil); an explicit empty list hides the line
func defaultStatusLineIDs() []string {
	return []string{
		"context", "model", "role", "effort", "yolo", "goal",
		"cwd", "git_branch", "cost", "total_tokens", "turn_timer", "expand", "hints",
	}
}

// shed order (first goes first) when the footer doesn't fit; identity items
// (context, model, role, safety) truncate instead
var statusDropOrder = []string{
	"hints", "total_tokens", "turn_timer", "expand", "git_branch", "cwd", "cost", "goal", "session_title",
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

// one drawn item and its edge
type statusPart struct {
	id    string
	text  string
	right bool
}

// configured items as one width-fitting row. the first right-aligned item
// opens the gap that pushes the rest right, so visible order = configured
// order. too wide: shed items in statusDropOrder before truncating
func (m Model) renderStatusLine() string {
	if m.statusLine == nil && m.statusLineAllOff() {
		return ""
	}
	ids := resolveStatusLine(m.statusLine)
	if len(ids) == 0 {
		return ""
	}
	var parts []statusPart
	rightOpen := false
	for _, id := range ids {
		it := statusRegistry[id]
		if it.align == alignRight {
			rightOpen = true
		}
		if s := it.draw(m); s != "" {
			parts = append(parts, statusPart{id: id, text: s, right: rightOpen})
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if m.width <= 0 {
		return statusJoin(parts, 0)
	}
	avail := m.width - 2 // gutter on both sides
	for _, id := range statusDropOrder {
		if lipglossWidth(statusJoin(parts, 0)) <= avail {
			break
		}
		parts = statusWithout(parts, id)
	}
	line := " " + statusJoin(parts, avail)
	if lipglossWidth(line) > m.width {
		line = truncateVisible(line, m.width)
	}
	return line
}

// left | right; width <= 0 measures the natural width without padding
func statusJoin(parts []statusPart, width int) string {
	var left, right []string
	for _, p := range parts {
		if p.right {
			right = append(right, p.text)
		} else {
			left = append(left, p.text)
		}
	}
	l, r := strings.Join(left, "  "), strings.Join(right, "  ")
	switch {
	case r == "":
		return l
	case l == "":
		if width > 0 {
			return strings.Repeat(" ", max(0, width-lipglossWidth(r))) + r
		}
		return r
	}
	pad := 2
	if width > 0 {
		pad = max(2, width-lipglossWidth(l)-lipglossWidth(r))
	}
	return l + strings.Repeat(" ", pad) + r
}

func statusWithout(parts []statusPart, id string) []statusPart {
	out := parts[:0:0]
	for _, p := range parts {
		if p.id != id {
			out = append(out, p)
		}
	}
	return out
}

// drawStatusModel renders the provider-qualified model label.
func drawStatusModel(m Model) string {
	if !m.showModel {
		return ""
	}
	return m.styles.Key.Render(m.displayModel())
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
