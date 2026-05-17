package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// claimByPane routes the message to whichever overlay pane is open and
// wants to consume it. Returns claimed=true when the caller should return
// immediately with the produced (Model, Cmd).
func (m Model) claimByPane(msg tea.Msg) (Model, tea.Cmd, bool) {
	// session tree modal claims keys while open.
	if m.tree != nil && m.tree.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newT, cmd := m.tree.Update(msg)
			m.tree = newT
			return m, cmd, true
		}
	}

	// resume picker modal claims keys while open.
	if m.resume != nil && m.resume.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newR, cmd := m.resume.Update(msg)
			m.resume = newR
			return m, cmd, true
		}
	}

	// cost pane claims keys while open.
	if m.costPane != nil && m.costPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newC, cmd := m.costPane.Update(msg)
			m.costPane = newC
			return m, cmd, true
		}
	}

	// model picker claims all input while active. modelsLoadedMsg /
	// spinner.TickMsg are routed unconditionally below so async loads still
	// settle into the cache.
	if m.picker != nil && m.picker.Active() {
		switch msg.(type) {
		case tea.KeyMsg, modelsLoadedMsg, spinner.TickMsg:
			newP, cmd := m.picker.Update(msg)
			m.picker = newP
			return m, cmd, true
		}
	}

	// login pane claims keys while open. async loginActionDoneMsg is
	// routed unconditionally below so the pane can clear its busy flag.
	if m.loginPane != nil && m.loginPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newL, cmd := m.loginPane.Update(msg)
			m.loginPane = newL
			return m, cmd, true
		}
	}

	// effort pane claims keys while open.
	if m.effortPane != nil && m.effortPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newE, cmd := m.effortPane.Update(msg)
			m.effortPane = newE
			return m, cmd, true
		}
	}

	// settings pane claims keys while open.
	if m.settingsPane != nil && m.settingsPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newS, cmd := m.settingsPane.Update(msg)
			m.settingsPane = newS
			return m, cmd, true
		}
	}

	// tools pane claims keys while open.
	if m.toolsPane != nil && m.toolsPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newT, cmd := m.toolsPane.Update(msg)
			m.toolsPane = newT
			return m, cmd, true
		}
	}

	// palette is active: nav keys go to palette, everything else falls
	// through to the main input so the user sees what they type. Filter
	// syncs from input value after handleKey runs (see KeyMsg branch).
	if m.palette.Active {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "up", "down", "enter", "esc", "ctrl+n", "ctrl+p", "ctrl+c":
				newP, cmd := m.palette.Update(msg)
				m.palette = newP
				return m, cmd, true
			}
		}
	}

	// @-picker claims keys while open.
	if m.atpicker.Active {
		if _, ok := msg.(tea.KeyMsg); ok {
			np, cmd := m.atpicker.Update(msg)
			m.atpicker = np
			return m, cmd, true
		}
	}

	// history picker claims keys while open.
	if m.history.Active {
		if _, ok := msg.(tea.KeyMsg); ok {
			nh, cmd := m.history.Update(msg)
			m.history = nh
			return m, cmd, true
		}
	}

	return m, nil, false
}
