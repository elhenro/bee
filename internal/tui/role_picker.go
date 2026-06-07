package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// roleLevel describes one row in the role picker.
type roleLevel struct {
	value string // worker | scout | queen
	label string // human-readable label
	desc  string // one-line description
}

var roleLevels = []roleLevel{
	{value: "worker", label: "worker", desc: "full tool surface; per-turn read|act classifier — the everyday default"},
	{value: "scout", label: "scout", desc: "read-only research + web; proposes a plan, never mutates"},
	{value: "queen", label: "queen", desc: "spawn a hive — decompose, sub-agents, verify, synthesize · best quality, slowest, most tokens"},
}

// RolePane is a modal picker for the agent role. Arrow keys pick, enter sets,
// esc closes without change.
type RolePane struct {
	open    bool
	cursor  int
	current string // current role value (for display)
}

// NewRolePane returns a closed role picker.
func NewRolePane() *RolePane { return &RolePane{current: "worker"} }

// Open reports visibility.
func (p *RolePane) Open() bool { return p != nil && p.open }

// Show opens the picker with the current role highlighted.
func (p *RolePane) Show(current string) {
	if p == nil {
		return
	}
	p.open = true
	p.cursor = 0
	p.current = current
	for i, e := range roleLevels {
		if e.value == current {
			p.cursor = i
			break
		}
	}
}

// SetCurrent updates the stored current value (called after commit).
func (p *RolePane) SetCurrent(v string) {
	if p != nil {
		p.current = v
	}
}

// rolePickedMsg is published when the user commits a role.
type rolePickedMsg string

// Update handles key events.
func (p *RolePane) Update(msg tea.Msg) (*RolePane, tea.Cmd) {
	if p == nil || !p.open {
		return p, nil
	}
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "esc", "q":
			p.open = false
		case "enter", " ":
			p.open = false
			v := roleLevels[p.cursor].value
			p.current = v
			return p, func() tea.Msg { return rolePickedMsg(v) }
		case "down", "j":
			if p.cursor < len(roleLevels)-1 {
				p.cursor++
			}
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		}
	}
	return p, nil
}

// View renders the modal.
func (p *RolePane) View(width, height int) string {
	if p == nil || !p.open {
		return ""
	}
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render("⬢ Agent Role")

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")

	for i, e := range roleLevels {
		marker := "  "
		nameStyle := lipgloss.NewStyle().Foreground(fgOyster)
		if i == p.cursor {
			marker = lipgloss.NewStyle().Foreground(accentHoney).Render("▸ ")
			nameStyle = nameStyle.Foreground(accentHoney).Bold(true)
		}
		b.WriteString(marker)
		b.WriteString(nameStyle.Render(padRightVisible(e.label, 8)))
		b.WriteString("  ")
		b.WriteString(StyleLabel.Render(e.desc))
		if e.value == p.current {
			b.WriteString("  ")
			b.WriteString(lipgloss.NewStyle().Foreground(accentHoney).Render("✓"))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ pick · enter set · esc close"))
	return boxModal(b.String(), width, height)
}
