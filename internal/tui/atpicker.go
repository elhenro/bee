package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AtPickerModel is the @-triggered fuzzy file picker. Renders inline above
// the input bar — same dense strip as the slash palette.
type AtPickerModel struct {
	Active   bool
	input    textinput.Model
	root     string
	selected int
	matches  []string
	width    int
}

// AtPickerSelectMsg is emitted when the user picks a path.
type AtPickerSelectMsg struct{ Path string }

// AtPickerDismissedMsg is emitted when the user escapes the picker.
type AtPickerDismissedMsg struct{}

// NewAtPicker returns an inactive picker rooted at root.
func NewAtPicker(root string) AtPickerModel {
	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.Prompt = "› "
	ti.Focus()
	return AtPickerModel{input: ti, root: root, matches: FuzzyFiles(root, "")}
}

// SetWidth tells the picker how wide a row may be.
func (p *AtPickerModel) SetWidth(w int) { p.width = w }

// Update handles picker key events.
func (p AtPickerModel) Update(msg tea.Msg) (AtPickerModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc", "ctrl+c":
			p.Active = false
			return p, func() tea.Msg { return AtPickerDismissedMsg{} }
		case "enter":
			if len(p.matches) == 0 {
				return p, nil
			}
			i := p.selected
			if i < 0 || i >= len(p.matches) {
				i = 0
			}
			path := p.matches[i]
			p.Active = false
			return p, func() tea.Msg { return AtPickerSelectMsg{Path: path} }
		case "down", "ctrl+n":
			if p.selected+1 < len(p.matches) {
				p.selected++
			}
			return p, nil
		case "up", "ctrl+p":
			if p.selected > 0 {
				p.selected--
			}
			return p, nil
		}
	}
	prevVal := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != prevVal {
		p.matches = FuzzyFiles(p.root, p.input.Value())
		p.selected = 0
	}
	return p, cmd
}

// maxAtPickerRows caps visible rows so the picker stays compact.
const maxAtPickerRows = 8

// View renders the picker as a dense strip above the input bar.
func (p AtPickerModel) View() string {
	if !p.Active {
		return ""
	}
	w := p.width
	if w <= 0 {
		w = 80
	}

	dim := lipgloss.NewStyle().Foreground(fgOyster)
	selMark := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	nameSel := lipgloss.NewStyle().Foreground(fgButter).Bold(true)
	nameNorm := lipgloss.NewStyle().Foreground(fgSmoke)
	glyph := lipgloss.NewStyle().Foreground(accentHoney)

	var b strings.Builder
	b.WriteString(glyph.Render("@") + " " + p.input.View())
	b.WriteString("\n")

	if len(p.matches) == 0 {
		b.WriteString(dim.Render("  no matches"))
		return b.String()
	}

	total := len(p.matches)
	start := 0
	if total > maxAtPickerRows {
		if p.selected >= maxAtPickerRows {
			start = p.selected - maxAtPickerRows + 1
		}
		if start+maxAtPickerRows > total {
			start = total - maxAtPickerRows
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + maxAtPickerRows
	if end > total {
		end = total
	}
	overflowAbove := start
	overflowBelow := total - end

	if overflowAbove > 0 {
		b.WriteString(dim.Render("  ↑ " + strconv.Itoa(overflowAbove) + " more"))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		var line strings.Builder
		if i == p.selected {
			line.WriteString(selMark.Render("›"))
		} else {
			line.WriteString(" ")
		}
		line.WriteString(" ")
		ns := nameNorm
		if i == p.selected {
			ns = nameSel
		}
		line.WriteString(ns.Render(p.matches[i]))
		row := line.String()
		if lipglossWidth(row) > w {
			row = truncateVisible(row, w)
		}
		b.WriteString(row)
		if i < end-1 || overflowBelow > 0 {
			b.WriteString("\n")
		}
	}
	if overflowBelow > 0 {
		b.WriteString(dim.Render("  ↓ " + strconv.Itoa(overflowBelow) + " more"))
	}
	return b.String()
}
