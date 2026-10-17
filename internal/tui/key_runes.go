package tui

import tea "github.com/charmbracelet/bubbletea"

// one key message per rune
func splitRunes(rs []rune) []tea.KeyMsg {
	keys := make([]tea.KeyMsg, len(rs))
	for i, r := range rs {
		keys[i] = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
	}
	return keys
}

// runs each rune through Update in order. in order matters: separate commands
// would race, and words arriving back to back would interleave
func (m Model) typeRunes(rs []rune) (tea.Model, tea.Cmd) {
	var nm tea.Model = m
	var cmds []tea.Cmd
	for _, k := range splitRunes(rs) {
		var cmd tea.Cmd
		nm, cmd = nm.Update(k)
		cmds = append(cmds, cmd)
	}
	return nm, tea.Batch(cmds...)
}
