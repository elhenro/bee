package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/commands"
)

// ToolsPane is a modal listing every registered tool with a toggle. Arrow
// keys move the cursor; enter/space flips the focused row. Each flip applies
// live and persists to ~/.bee/config.toml under disabled_tools. Mirrors the
// SettingsPane shape so users see the same modal grammar.
type ToolsPane struct {
	open   bool
	cursor int
	rows   []commands.ToolInfo
}

// NewToolsPane returns a closed tools pane.
func NewToolsPane() *ToolsPane { return &ToolsPane{} }

// Open reports visibility.
func (p *ToolsPane) Open() bool { return p != nil && p.open }

// Show opens the pane seeded with the live tool list.
func (p *ToolsPane) Show(rows []commands.ToolInfo) {
	if p == nil {
		return
	}
	p.open = true
	p.cursor = 0
	p.rows = rows
}

// toolsToggleMsg is published when a row is toggled. Carries the tool name
// and the new disabled value so Model.Update can apply + persist atomically.
type toolsToggleMsg struct {
	name     string
	disabled bool
}

// Update handles key events.
func (p *ToolsPane) Update(msg tea.Msg) (*ToolsPane, tea.Cmd) {
	if p == nil || !p.open {
		return p, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch km.String() {
	case "esc", "q":
		p.open = false
	case "down", "j":
		if p.cursor < len(p.rows)-1 {
			p.cursor++
		}
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "enter", " ", "tab":
		if p.cursor >= len(p.rows) {
			return p, nil
		}
		row := &p.rows[p.cursor]
		row.Disabled = !row.Disabled
		name := row.Name
		dis := row.Disabled
		return p, func() tea.Msg { return toolsToggleMsg{name: name, disabled: dis} }
	}
	return p, nil
}

// View renders the modal.
func (p *ToolsPane) View(width, height int) string {
	if p == nil || !p.open {
		return ""
	}
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render("⬢ Tools")

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")

	if len(p.rows) == 0 {
		b.WriteString(StyleLabel.Render("(no tools registered)"))
		b.WriteString("\n")
		return boxModal(b.String(), width, height)
	}

	nameWidth := 0
	for _, r := range p.rows {
		if len(r.Name) > nameWidth {
			nameWidth = len(r.Name)
		}
	}
	for i, r := range p.rows {
		marker := "  "
		nameStyle := lipgloss.NewStyle().Foreground(fgOyster)
		if i == p.cursor {
			marker = lipgloss.NewStyle().Foreground(accentHoney).Render("▸ ")
			nameStyle = nameStyle.Foreground(accentHoney).Bold(true)
		}
		toggle := lipgloss.NewStyle().Foreground(accentHoney).Render("[x]")
		if r.Disabled {
			toggle = "[ ]"
		}
		src := "builtin"
		if r.UserDefined {
			src = "user"
		}
		b.WriteString(marker)
		b.WriteString(toggle)
		b.WriteString("  ")
		b.WriteString(nameStyle.Render(padRightVisible(r.Name, nameWidth)))
		b.WriteString("  ")
		b.WriteString(StyleLabel.Render(padRightVisible(src, 7)))
		b.WriteString("  ")
		b.WriteString(StyleLabel.Render(truncateOneLine(r.Description, 60)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ pick · enter/space toggle · esc close · /tools add NAME CMD adds new"))
	return boxModal(b.String(), width, height)
}

// truncateOneLine collapses to one line and hard-trims to max runes.
func truncateOneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
