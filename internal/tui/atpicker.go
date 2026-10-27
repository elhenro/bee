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
	// index is nil until the async walk lands; keystrokes before that only
	// edit the filter text and apply once it arrives
	index *fileIndex
}

// atIndexMsg delivers the finished file walk for root.
type atIndexMsg struct {
	root  string
	index *fileIndex
}

// AtPickerSelectMsg is emitted when the user picks a path.
type AtPickerSelectMsg struct{ Path string }

// AtPickerDismissedMsg is emitted when the user escapes the picker.
type AtPickerDismissedMsg struct{}

// NewAtPicker returns an inactive picker rooted at root. Its file list fills
// in once the cmd from indexCmd lands.
func NewAtPicker(root string) AtPickerModel {
	ti := textinput.New()
	ti.Placeholder = "filter…"
	ti.Prompt = " › "
	ti.Focus()
	return AtPickerModel{input: ti, root: root}
}

// indexCmd walks the tree off the UI goroutine.
func (p AtPickerModel) indexCmd() tea.Cmd {
	root := p.root
	return func() tea.Msg { return atIndexMsg{root: root, index: indexFiles(root)} }
}

// withIndex applies a finished walk. A walk for another root, or one landing
// after the picker closed, is dropped.
func (p AtPickerModel) withIndex(msg atIndexMsg) AtPickerModel {
	if !p.Active || msg.root != p.root {
		return p
	}
	p.index = msg.index
	p.matches = p.index.filter(p.input.Value())
	p.selected = 0
	return p
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
		p.matches = p.index.filter(p.input.Value())
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
	lines := []string{" " + lipgloss.NewStyle().Foreground(accentHoney).Render("@") + " " + p.input.View()}
	total := len(p.matches)
	if total == 0 {
		note := "   no matches"
		if p.index == nil {
			note = "   indexing…"
		}
		return strings.Join(append(lines, dim.Render(note)), "\n")
	}
	start, end := scrollWindow(total, p.selected, maxAtPickerRows)
	if start > 0 {
		lines = append(lines, dim.Render("   ↑ "+strconv.Itoa(start)+" more"))
	}
	for i := start; i < end; i++ {
		mark, name := "  ", lipgloss.NewStyle().Foreground(fgSmoke)
		if i == p.selected {
			mark, name = " "+StyleActive.Render(glyphSel), lipgloss.NewStyle().Foreground(fgButter).Bold(true)
		}
		lines = append(lines, truncateVisible(mark+" "+name.Render(p.matches[i]), w))
	}
	if end < total {
		lines = append(lines, dim.Render("   ↓ "+strconv.Itoa(total-end)+" more"))
	}
	return strings.Join(lines, "\n")
}
