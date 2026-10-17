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
	id     string
	align  statusAlign
	draw   func(m Model) string
	narrow func(m Model) string // shorter form tried before the row truncates; nil = none
}

// statusRegistry is the id → item table and the single source of truth for
// which status-line items exist. Adding an item here is all it takes to make
// it selectable via tui.status_line. Later cards layer on top of it (machine
// mode, narrow-terminal fitting, alias resolution) without changing callers.
var statusRegistry = map[string]statusItem{
	"context":       {id: "context", align: alignLeft, draw: func(m Model) string { return m.renderContextHex() }},
	"model":         {id: "model", align: alignLeft, draw: drawStatusModel, narrow: drawStatusModelShort},
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

// last resort on a narrow terminal, after the model name is shortened: the
// chips go before the model, context gauge or safety badge ever truncate
var statusNarrowDrop = []string{"effort", "role"}

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
	fits := func() bool { return lipglossWidth(statusJoin(parts, 0)) <= avail }
	shed := func(ids []string) {
		for _, id := range ids {
			if fits() {
				return
			}
			parts = statusWithout(parts, id)
		}
	}
	shed(statusDropOrder)
	if !fits() {
		for i, p := range parts {
			if nar := statusRegistry[p.id].narrow; nar != nil {
				if t := nar(m); t != "" {
					parts[i].text = t
				}
			}
		}
		shed(statusNarrowDrop)
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
