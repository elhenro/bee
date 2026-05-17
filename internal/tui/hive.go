package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/types"
)

// BeeState is the lifecycle of one bee (session).
type BeeState int

const (
	Active BeeState = iota
	Awaiting
	Idle
	Done
	Failed
)

var beeStateNames = map[BeeState]string{Active: "active", Awaiting: "awaiting", Idle: "idle", Done: "done", Failed: "failed"}

func (s BeeState) String() string {
	if n, ok := beeStateNames[s]; ok {
		return n
	}
	return "unknown"
}

// Bee is one entry in the hive — either a live in-process session or a
// historical session pulled from disk via session.List().
type Bee struct {
	Name      string
	State     BeeState
	Model     string
	SessionID string
	StartedAt time.Time
}

// Hive is the bubbletea component that owns the hex strip and the full
// `Ctrl+H` view. It does not spawn bees itself (Wave 4); for v0.1 the data
// list is populated from session.List() plus whatever the host model
// registers via Set / Upsert.
type Hive struct {
	bees     []Bee
	expanded bool
	maxStrip int // soft cap on strip entries before truncation
}

// NewHive returns a Hive seeded with on-disk sessions (best-effort).
// 3A constructs this via `NewHive()` and embeds it into its Model.
func NewHive() *Hive {
	h := &Hive{maxStrip: 8}
	if sessions, err := session.List(); err == nil {
		h.loadSessions(sessions)
	}
	return h
}

func (h *Hive) loadSessions(sessions []types.Session) {
	// Sort newest-first; most-recent shown first in strip.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].Created.After(sessions[j].Created)
	})
	for _, s := range sessions {
		h.bees = append(h.bees, Bee{
			Name:      shortName(s.ID),
			State:     Done,
			Model:     s.Model,
			SessionID: s.ID,
			StartedAt: s.Created,
		})
	}
}

// Init satisfies tea.Model.
func (h *Hive) Init() tea.Cmd { return nil }

// Update handles toggle messages for the full view. The host model owns the
// keybinding (Ctrl+H) and forwards a ToggleHiveMsg.
func (h *Hive) Update(msg tea.Msg) (*Hive, tea.Cmd) {
	switch msg.(type) {
	case ToggleHiveMsg:
		h.expanded = !h.expanded
	}
	return h, nil
}

// ToggleHiveMsg flips the full-screen hive view.
type ToggleHiveMsg struct{}

// Expanded reports whether the full view is open.
func (h *Hive) Expanded() bool { return h.expanded }

// Bees returns a copy of the current bee list (for callers / tests).
func (h *Hive) Bees() []Bee {
	out := make([]Bee, len(h.bees))
	copy(out, h.bees)
	return out
}

// Set replaces the bee list (used by 3A or hive spawner).
func (h *Hive) Set(bees []Bee) {
	h.bees = append(h.bees[:0], bees...)
}

// Upsert inserts or updates a bee by SessionID (or Name fallback).
func (h *Hive) Upsert(b Bee) {
	key := b.SessionID
	if key == "" {
		key = b.Name
	}
	for i := range h.bees {
		k := h.bees[i].SessionID
		if k == "" {
			k = h.bees[i].Name
		}
		if k == key {
			h.bees[i] = b
			return
		}
	}
	h.bees = append(h.bees, b)
}

// Render returns the one-line hex strip. Active first, awaiting next, then
// idle, then done. Truncates to maxStrip entries; further capped by width.
func (h *Hive) Render(width int) string {
	if len(h.bees) == 0 {
		return StyleLabel.Render("⬡ no bees yet")
	}
	ordered := sortForStrip(h.bees)
	if h.maxStrip > 0 && len(ordered) > h.maxStrip {
		ordered = ordered[:h.maxStrip]
	}
	states := make([]BeeState, len(ordered))
	names := make([]string, len(ordered))
	for i, b := range ordered {
		states[i] = b.State
		names[i] = b.Name
	}
	row := HexRow(states, names, width)
	label := StyleLabel.Render(" hive")
	if width > 0 && lipglossWidth(row)+lipglossWidth(label) <= width {
		return row + lipgloss.NewStyle().Render(strings.Repeat(" ", max(1, width-lipglossWidth(row)-lipglossWidth(label)))) + label
	}
	return row
}

// RenderFull renders the Ctrl+H full hive view: a 3-wide hex grid of active
// bees followed by a list of recent sessions.
func (h *Hive) RenderFull(width, height int) string {
	title := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ColorAccent)).
		Bold(true).
		Render("⬢ Hive · all bees")
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")

	active := filterByState(h.bees, Active, Awaiting, Idle)
	if len(active) == 0 {
		b.WriteString(StyleLabel.Render("no live bees — spawn one with `bee fan` or `bee swarm`"))
		b.WriteString("\n")
	} else {
		b.WriteString(renderHexGrid(active, 3))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("recent sessions"))
	b.WriteString("\n")
	recent := filterByState(h.bees, Done, Failed)
	if len(recent) == 0 {
		b.WriteString(StyleLabel.Render("  (none)"))
	} else {
		max := 10
		if len(recent) < max {
			max = len(recent)
		}
		for _, bee := range recent[:max] {
			line := fmt.Sprintf("  %s %s  %s",
				StyleDone.Render(HexHollow),
				bee.Name,
				StyleLabel.Render(bee.SessionID),
			)
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// renderHexGrid lays out bees in a per-row count grid.
func renderHexGrid(bees []Bee, perRow int) string {
	var b strings.Builder
	for i := 0; i < len(bees); i += perRow {
		end := i + perRow
		if end > len(bees) {
			end = len(bees)
		}
		var cells []string
		for _, bee := range bees[i:end] {
			cells = append(cells, renderHexCard(bee))
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		b.WriteString("\n")
	}
	return b.String()
}

func renderHexCard(b Bee) string {
	style := StyleActive
	glyph := HexFilled
	switch b.State {
	case Awaiting:
		style = StyleAwaiting
	case Idle:
		style, glyph = StyleIdle, HexHollow
	case Done:
		style, glyph = StyleDone, HexHollow
	case Failed:
		style = StyleFailed
	}
	body := fmt.Sprintf("%s %s\n%s\n%s",
		style.Render(glyph),
		style.Render(b.Name),
		StyleLabel.Render(b.Model),
		StyleLabel.Render(b.State.String()),
	)
	return lipgloss.NewStyle().
		Padding(0, 2).
		MarginRight(2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(ColorAmber)).
		Render(body)
}

// stripOrder ranks states for the bottom strip ordering.
var stripOrder = map[BeeState]int{Active: 0, Awaiting: 1, Idle: 2, Done: 3, Failed: 4}

func sortForStrip(in []Bee) []Bee {
	out := append([]Bee(nil), in...)
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := stripOrder[out[i].State], stripOrder[out[j].State]; a != b {
			return a < b
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	return out
}

func filterByState(in []Bee, want ...BeeState) []Bee {
	m := make(map[BeeState]bool, len(want))
	for _, s := range want {
		m[s] = true
	}
	var out []Bee
	for _, b := range in {
		if m[b.State] {
			out = append(out, b)
		}
	}
	return out
}

func shortName(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
