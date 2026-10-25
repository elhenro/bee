package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// choiceLevel describes one row in a choice pane.
type choiceLevel struct {
	value string // committed value
	label string // human-readable label
	desc  string // one-line description
}

// choicePane is a modal picker over a fixed list. Arrow keys pick, enter
// commits (publishing picked(value)), esc closes without change.
type choicePane struct {
	title   string
	levels  []choiceLevel
	picked  func(value string) tea.Msg
	open    bool
	cursor  int
	current string // current value (for display)
}

// Open reports visibility.
func (p *choicePane) Open() bool { return p != nil && p.open }

// Show opens the picker with the current value highlighted.
func (p *choicePane) Show(current string) {
	if p == nil {
		return
	}
	p.open = true
	p.cursor = 0
	p.current = current
	for i, e := range p.levels {
		if e.value == current {
			p.cursor = i
			break
		}
	}
}

// SetCurrent updates the stored current value (called after commit).
func (p *choicePane) SetCurrent(v string) {
	if p != nil {
		p.current = v
	}
}

// Update handles key events.
func (p *choicePane) Update(msg tea.Msg) (*choicePane, tea.Cmd) {
	if p == nil || !p.open {
		return p, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc", "q":
			p.open = false
		case "enter", " ":
			p.open = false
			v := p.levels[p.cursor].value
			p.current = v
			return p, func() tea.Msg { return p.picked(v) }
		case "down", "j":
			if p.cursor < len(p.levels)-1 {
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
func (p *choicePane) View(width, height int) string {
	if p == nil || !p.open {
		return ""
	}
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render(p.title)

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")

	for i, e := range p.levels {
		marker := "  "
		nameStyle := lipgloss.NewStyle().Foreground(fgOyster)
		if i == p.cursor {
			marker = selectMark(true)
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
	return boxModal(b.String(), width)
}
