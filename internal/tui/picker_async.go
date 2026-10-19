package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// HandleAsync feeds the picker its background messages (model fetch results,
// spinner ticks) for hosts outside this package, where those types are
// unexported. handled is false for anything else.
func (p *Picker) HandleAsync(msg tea.Msg) (_ *Picker, _ tea.Cmd, handled bool) {
	switch msg.(type) {
	case modelsLoadedMsg, spinner.TickMsg:
		np, cmd := p.Update(msg)
		return np, cmd, true
	}
	return p, nil, false
}
