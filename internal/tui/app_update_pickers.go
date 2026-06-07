package tui

import (
	"context"
	"strings"
	"time"

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
	// rescue routing: a pick made after /handoff goes to the handoff path
	// instead of a plain swap+persist. Consume the sentinel exactly once.
	if m.handoffActive {
		m.handoffActive = false
		return m.onHandoffPicked(msg)
	}
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

func (m Model) onRolePicked(msg rolePickedMsg) (tea.Model, tea.Cmd) {
	v := string(msg)
	wasQueen := m.role == "queen"
	if err := m.side().SetRole(v); err != nil {
		m.lastErr = err.Error()
		m.state = StateError
		return m, nil
	}
	// SetRole updated m.role + baked thinking already; mirror the row.
	m.rolePane.SetCurrent(m.role)
	// only on the →queen transition: arm exactly one glow loop and warn that
	// every turn now spawns a hive. Re-picking queen while already on it must
	// not stack a second tick loop.
	if m.role == "queen" && !wasQueen {
		m.warning = "queen: every turn now spawns a hive — slower, higher quality, more tokens"
		m.warningExpires = time.Now().Add(warningTTL)
		return m, tea.Batch(glowTickCmd(), warningFadeCmd())
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
	// SetThinking canonicalized m.thinking already; mirror the current row.
	m.effortPane.SetCurrent(m.thinking)
	return m, nil
}

// onHandoffPicked stops the stuck turn and kicks off async brief generation on
// the pre-switch (small) provider. The model switch is deferred to
// onHandoffReady so Rebuild never races the summarization goroutine.
func (m Model) onHandoffPicked(msg PickedMsg) (tea.Model, tea.Cmd) {
	if m.eng == nil {
		m.lastErr = "handoff: no engine"
		m.state = StateError
		return m, nil
	}
	// stop the in-flight stuck turn; bump the epoch so its late (Canceled)
	// turnDoneMsg is ignored and can't clobber the handoff.
	if m.cancelRun != nil {
		m.cancelRun()
		m.cancelRun = nil
	}
	m.turnGen++
	m.state = StateIdle
	// capture the stall signal: the live error if present, else the reason
	// preserved across a prior StateError recovery (handleSubmit stashes it).
	stall := m.lastErr
	if stall == "" {
		stall = m.handoffStall
	}
	m.lastErr = ""
	m.handoffStall = ""
	m.handoffing = true
	m.loaderFrame = 0
	snapshot := nonEphemeral(m.messages)
	partial := m.partial
	eng := m.eng
	ctx := m.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	prov, model := msg.Provider, msg.Model
	return m, tea.Batch(
		loaderTickCmd(),
		func() tea.Msg {
			brief, err := eng.Handoff(ctx, snapshot, partial, stall)
			return handoffReadyMsg{brief: brief, provider: prov, model: model, err: err}
		},
	)
}

// onHandoffReady applies the model switch (now that the brief is built on the
// small model), drops the stuck transcript, and injects the brief as the next
// user turn so the big model continues from a clean slate.
func (m Model) onHandoffReady(msg handoffReadyMsg) (tea.Model, tea.Cmd) {
	m.handoffing = false
	if msg.err != nil {
		m.lastErr = "handoff: " + msg.err.Error()
		m.state = StateError
		return m, nil
	}
	if err := m.side().SwitchProviderModel(msg.provider, msg.model); err != nil {
		m.lastErr = err.Error()
		m.state = StateError
		return m, nil
	}
	if perr := PersistPick("", msg.provider, msg.model); perr != nil {
		m.lastErr = "saved live but persist failed: " + perr.Error()
	}
	// drop the stuck transcript: the brief already carries goal + what was tried
	// + blocker + last steps. Fresh slice + printedCount=0 keeps already-printed
	// scrollback on screen while the big model's context starts clean.
	m.messages = nil
	m.printedCount = 0
	if m.eng != nil {
		m.eng.InitialMessages = nil
	}
	// inject the brief as the next user turn; display shows the compact tag.
	return m.submitWithDisplay(msg.brief, "/handoff → "+msg.model)
}
