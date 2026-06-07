package queen

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run launches the supervisor program. Blocks until the operator quits (q
// after all bees finish, or ctrl+c/ctrl+d). The caller redirects worker
// engine output away from stdout before this runs.
func (m *Model) Run() error {
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
