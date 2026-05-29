package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) onOpenPalette(_ openPaletteMsg) (tea.Model, tea.Cmd) {
	if m.cmds != nil {
		// stage "/" in the main input so user sees a live query line.
		// palette mirrors filter from input value after each keystroke.
		if !strings.HasPrefix(m.input.Value(), "/") {
			m.input.SetValue("/")
			m.input.CursorEnd()
		}
		m.palette.Show(strings.TrimPrefix(m.input.Value(), "/"))
	}
	return m, nil
}

func (m Model) onPaletteSelect(msg PaletteSelectMsg) (tea.Model, tea.Cmd) {
	// commands AND skills both submit immediately via "/name" — runSlash
	// dispatches to the command registry first, then falls through to the
	// skill registry. unified path keeps "/calc" and "#calc → enter"
	// behaving the same.
	// preserve any args typed after the command name so
	// "/research golang webfetch" reaches the dispatcher intact.
	args := ""
	if rest := strings.TrimPrefix(m.input.Value(), "/"); rest != "" {
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			args = rest[i:] // includes leading space
		}
	}
	m.input.SetValue("/" + msg.Name + args)
	return m.handleSubmit()
}

func (m Model) onPaletteDismissed(_ PaletteDismissedMsg) (tea.Model, tea.Cmd) {
	// clear the slash-query staged in the input on esc — the user
	// cancelled the palette, no reason to leave "/foo" behind.
	if strings.HasPrefix(m.input.Value(), "/") {
		m.input.Reset()
	}
	return m, nil
}

func (m Model) onAtPickerSelect(msg AtPickerSelectMsg) (tea.Model, tea.Cmd) {
	// replace last `@partial` with the picked path. textarea exposes
	// only column-cursor, not row+col SetCursor, so we set the value
	// and land the cursor at end of buffer.
	val := m.input.Value()
	atIdx := strings.LastIndex(val, "@")
	if atIdx < 0 {
		m.input.SetValue(val + msg.Path)
	} else {
		m.input.SetValue(val[:atIdx] + msg.Path)
	}
	return m, nil
}

func (m Model) onHistorySelect(msg HistorySelectMsg) (tea.Model, tea.Cmd) {
	// paste into the main input; user can edit then submit.
	m.input.SetValue(msg.Text)
	m.input.CursorEnd()
	return m, nil
}

func (m Model) onOpenProvider(_ openProviderMsg) (tea.Model, tea.Cmd) {
	if m.picker == nil {
		return m, nil
	}
	// resize to current frame so columns aren't 0-width on first open
	if m.width > 0 && m.height > 0 {
		m.picker.SetSize(m.width-4, m.height-4)
	}
	return m, m.picker.Show()
}

func (m Model) onPicked(msg PickedMsg) (tea.Model, tea.Cmd) {
	if err := m.side().SwitchProviderModel(msg.Provider, msg.Model); err != nil {
		m.lastErr = err.Error()
		m.state = StateError
		return m, nil
	}
	// persist for next launch; non-fatal if it fails (e.g. read-only fs)
	if perr := PersistPick("", msg.Provider, msg.Model); perr != nil {
		m.lastErr = "saved live but persist failed: " + perr.Error()
		m.state = StateError
	}
	return m, nil
}

func (m Model) onPickerLoginRequested(msg PickerLoginRequestedMsg) (tea.Model, tea.Cmd) {
	// picker hit an auth error and user pressed ctrl+l. Open the login
	// pane scoped to the failing provider so they can paste a key inline.
	if m.loginPane != nil {
		m.loginPane.Show()
		m.loginPane.SelectProvider(msg.Provider)
	}
	return m, nil
}

func (m Model) onEffortPicked(msg effortPickedMsg) (tea.Model, tea.Cmd) {
	v := string(msg)
	if err := m.side().SetThinking(v); err != nil {
		m.lastErr = err.Error()
		m.state = StateError
		return m, nil
	}
	m.thinking = v
	m.effortPane.SetCurrent(v)
	return m, nil
}
