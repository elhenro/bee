package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// effortLevel describes one row in the effort picker.
type effortLevel struct {
	value string // off | low | medium | high
	label string // human-readable label
	desc  string // one-line description
}

var effortLevels = []effortLevel{
	{value: "auto", label: "auto", desc: "medium for reasoning models, off otherwise — default"},
	{value: "off", label: "off", desc: "no reasoning tokens — fastest responses"},
	{value: "low", label: "low", desc: "minimal reasoning — balanced speed/quality"},
	{value: "medium", label: "medium", desc: "moderate reasoning"},
	{value: "high", label: "high", desc: "deep reasoning — best quality, slowest"},
}

// EffortPane is a modal picker for reasoning effort. Arrow keys pick, enter
// sets, esc closes without change.
type EffortPane struct {
	open    bool
	cursor  int
	current string // current effort value (for display)
}

// NewEffortPane returns a closed effort picker.
func NewEffortPane() *EffortPane { return &EffortPane{current: "off"} }

// Open reports visibility.
func (p *EffortPane) Open() bool { return p != nil && p.open }

// Show opens the picker with the current effort highlighted.
func (p *EffortPane) Show(current string) {
	if p == nil {
		return
	}
	p.open = true
	p.cursor = 0
	p.current = current
	for i, e := range effortLevels {
		if e.value == current {
			p.cursor = i
			break
		}
	}
}

// SetCurrent updates the stored current value (called after commit).
func (p *EffortPane) SetCurrent(v string) {
	if p != nil {
		p.current = v
	}
}

// PickedMsg is published when user commits an effort level.
type effortPickedMsg string

// Update handles key events.
func (p *EffortPane) Update(msg tea.Msg) (*EffortPane, tea.Cmd) {
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
			v := effortLevels[p.cursor].value
			p.current = v
			return p, func() tea.Msg { return effortPickedMsg(v) }
		case "down", "j":
			if p.cursor < len(effortLevels)-1 {
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
func (p *EffortPane) View(width, height int) string {
	if p == nil || !p.open {
		return ""
	}
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render("⬢ Reasoning Effort")

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")

	for i, e := range effortLevels {
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
