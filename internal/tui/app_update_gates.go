package tui

import (
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// keyPane is an overlay pane that claims key presses while open.
type keyPane[P any] interface {
	comparable
	Open() bool
	Update(tea.Msg) (P, tea.Cmd)
}

// claimKey feeds a key press to the pane at *p when it is set and open.
func claimKey[P keyPane[P]](p *P, msg tea.Msg) (cmd tea.Cmd, claimed bool) {
	var unset P
	if _, ok := msg.(tea.KeyMsg); !ok || *p == unset || !(*p).Open() {
		return nil, false
	}
	*p, cmd = (*p).Update(msg)
	return cmd, true
}

// claimByPane routes the message to whichever overlay pane is open and
// wants to consume it. Returns claimed=true when the caller should return
// immediately with the produced (Model, Cmd).
func (m Model) claimByPane(msg tea.Msg) (Model, tea.Cmd, bool) {
	// session tree, resume, rewind, cost and usage panes claim keys while open.
	if cmd, ok := claimKey(&m.tree, msg); ok {
		return m, cmd, true
	}
	if cmd, ok := claimKey(&m.resume, msg); ok {
		return m, cmd, true
	}
	if cmd, ok := claimKey(&m.rewind, msg); ok {
		return m, cmd, true
	}
	if cmd, ok := claimKey(&m.costPane, msg); ok {
		return m, cmd, true
	}
	if cmd, ok := claimKey(&m.usagePane, msg); ok {
		return m, cmd, true
	}

	// model picker claims all input while active. a finished model fetch is
	// routed even after the picker closed so it still settles into the cache.
	if lm, ok := msg.(modelsLoadedMsg); ok && m.picker != nil {
		newP, cmd := m.picker.Update(lm)
		m.picker = newP
		return m, cmd, true
	}
	if m.picker != nil && m.picker.Active() {
		switch msg.(type) {
		case tea.KeyMsg, spinner.TickMsg:
			newP, cmd := m.picker.Update(msg)
			m.picker = newP
			return m, cmd, true
		}
	}

	// login pane claims keys while open. async loginActionDoneMsg is
	// routed unconditionally below so the pane can clear its busy flag.
	if cmd, ok := claimKey(&m.loginPane, msg); ok {
		return m, cmd, true
	}
	// role, effort, settings and tools panes claim keys while open.
	if cmd, ok := claimKey(&m.rolePane, msg); ok {
		return m, cmd, true
	}
	if cmd, ok := claimKey(&m.effortPane, msg); ok {
		return m, cmd, true
	}
	if cmd, ok := claimKey(&m.settingsPane, msg); ok {
		return m, cmd, true
	}
	if cmd, ok := claimKey(&m.toolsPane, msg); ok {
		return m, cmd, true
	}

	// palette is active: nav keys go to palette, everything else falls
	// through to the main input so the user sees what they type. Filter
	// syncs from input value after handleKey runs (see KeyMsg branch).
	if m.palette.Active {
		if km, ok := msg.(tea.KeyMsg); ok {
			// enter with nothing to pick closes the palette and lets the
			// typed line submit, else a prompt starting with "/" is swallowed
			if km.String() == "enter" && len(m.palette.matches) == 0 {
				m.palette.Active = false
				return m, nil, false
			}
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
