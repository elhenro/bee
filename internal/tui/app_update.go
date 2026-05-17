package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/types"
)

// Update is the bubbletea main switch.
func (m Model) Update(msg tea.Msg) (resultModel tea.Model, resultCmd tea.Cmd) {
	// pre-grow textarea so any input mutation in this turn (handleKey,
	// SetValue from a palette/picker, etc.) wraps inside a tall viewport
	// instead of scrolling YOffset down and hiding row 0. The defer below
	// shrinks back to the actual row count after the message is processed
	// so the persistent model carries the layout-accurate height.
	m.inputGrowForMutation()
	// shrink textarea back to actual row count once the message is processed
	// so the persistent model carries the layout-accurate height. Runs even
	// on the early returns below (quit gates, pane claims).
	defer func() {
		if mm, ok := resultModel.(Model); ok {
			mm.syncInputHeight()
			resultModel = mm
		}
	}()
	// global hard-quit gate — runs above every pane/modal so the user is
	// never trapped. ctrl+c quits immediately (POSIX cancel convention);
	// ctrl+d requires two presses within quitConfirmWindow. Any other key
	// disarms the confirm so a stray ctrl+d in the input bar doesn't leave
	// the program one-keystroke from death.
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "ctrl+c":
			if m.cancelRun != nil {
				m.cancelRun()
			}
			return m, tea.Quit
		case "ctrl+d":
			if m.quitArmed && time.Since(m.quitArmedAt) <= quitConfirmWindow {
				if m.cancelRun != nil {
					m.cancelRun()
				}
				return m, tea.Quit
			}
			m.quitArmed = true
			m.quitArmedAt = time.Now()
			return m, nil
		default:
			if m.quitArmed {
				m.quitArmed = false
			}
		}
	}
	// Dangerous-command prompt arrives from the engine goroutine via the
	// Approver adapter. Surface the modal so the user can pick.
	if ask, ok := msg.(ApprovalAskMsg); ok {
		m.approval.Show(ApprovalRequest{
			ToolName: "bash",
			Action:   ask.Cmd,
			Reason:   ask.Reason,
			Key:      ask.Key,
			UseID:    ask.UseID,
		})
		return m, nil
	}
	// modal first: it consumes keys when active.
	if m.approval.Active {
		newApp, cmd := m.approval.Update(msg)
		m.approval = newApp
		if dec, ok := msg.(ApprovalDecisionMsg); ok {
			m.state = StateIdle
			if m.approver != nil {
				m.approver.Resolve(dec.UseID, dec.Decision)
			}
		}
		return m, cmd
	}

	// session tree modal claims keys while open.
	if m.tree != nil && m.tree.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newT, cmd := m.tree.Update(msg)
			m.tree = newT
			return m, cmd
		}
	}

	// resume picker modal claims keys while open.
	if m.resume != nil && m.resume.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newR, cmd := m.resume.Update(msg)
			m.resume = newR
			return m, cmd
		}
	}

	// cost pane claims keys while open.
	if m.costPane != nil && m.costPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newC, cmd := m.costPane.Update(msg)
			m.costPane = newC
			return m, cmd
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
			return m, cmd
		}
	}

	// login pane claims keys while open. async loginActionDoneMsg is
	// routed unconditionally below so the pane can clear its busy flag.
	if m.loginPane != nil && m.loginPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newL, cmd := m.loginPane.Update(msg)
			m.loginPane = newL
			return m, cmd
		}
	}

	// effort pane claims keys while open.
	if m.effortPane != nil && m.effortPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newE, cmd := m.effortPane.Update(msg)
			m.effortPane = newE
			return m, cmd
		}
	}

	// settings pane claims keys while open.
	if m.settingsPane != nil && m.settingsPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newS, cmd := m.settingsPane.Update(msg)
			m.settingsPane = newS
			return m, cmd
		}
	}

	// tools pane claims keys while open.
	if m.toolsPane != nil && m.toolsPane.Open() {
		if _, ok := msg.(tea.KeyMsg); ok {
			newT, cmd := m.toolsPane.Update(msg)
			m.toolsPane = newT
			return m, cmd
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
				return m, cmd
			}
		}
	}

	// @-picker claims keys while open.
	if m.atpicker.Active {
		if _, ok := msg.(tea.KeyMsg); ok {
			np, cmd := m.atpicker.Update(msg)
			m.atpicker = np
			return m, cmd
		}
	}

	// history picker claims keys while open.
	if m.history.Active {
		if _, ok := msg.(tea.KeyMsg); ok {
			nh, cmd := m.history.Update(msg)
			m.history = nh
			return m, cmd
		}
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// re-size text input to fit
		m.input.SetWidth(max(0, msg.Width-4))
		m.stream = NewStreamRenderer(m.styles, max(40, msg.Width-2))
		m.stream.SetVerbose(m.verbose)
		m.stream.SetShowThoughts(m.showThoughts)
		m.stream.SetShowNudges(m.showNudges)
		m.stream.SetCompact(m.compact)
		m.stream.SetHighlight(m.highlight)
		m.palette.SetWidth(msg.Width)
		m.atpicker.SetWidth(msg.Width)
		if m.picker != nil {
			m.picker.SetSize(msg.Width-4, msg.Height-4)
		}
		return m, nil

	case tea.KeyMsg:
		nm, cmd := m.handleKey(msg)
		// palette filter mirrors the main input. Close palette if the user
		// backspaced past the leading "/".
		if mm, ok := nm.(Model); ok && mm.palette.Active {
			val := mm.input.Value()
			if strings.HasPrefix(val, "/") {
				mm.palette.SetFilter(val[1:])
			} else {
				mm.palette.Active = false
			}
			return mm, cmd
		}
		return nm, cmd

	case streamDeltaMsg:
		// append to live partial. View() picks it up next render. The pump
		// re-arms itself so subsequent deltas keep draining.
		m.partial += msg.Delta
		// newline-gated head flush: only check the budget when this delta
		// completed a line. Tiny per-character deltas skip the work; line
		// terminators trigger the overflow check + possible scrollback push.
		var flushCmd tea.Cmd
		if strings.ContainsRune(msg.Delta, '\n') {
			flushCmd = m.maybeFlushPartialHead()
		}
		if flushCmd == nil {
			return m, m.waitStream()
		}
		return m, tea.Batch(flushCmd, m.waitStream())

	case liveMsgMsg:
		// engine persisted a new assistant/tool message mid-Run; print it to
		// native scrollback right away so the user sees tool cards as they
		// happen instead of only at turnDoneMsg. clear m.partial because the
		// assistant's text is now part of the appended ContentBlock —
		// leaving the live buffer would double-render the same text. Dedupe
		// by ID so a turnDoneMsg replacement followed by a late-arriving
		// live msg doesn't double-add.
		if msg.Msg.ID != "" {
			for _, existing := range m.messages {
				if existing.ID == msg.Msg.ID {
					return m, m.waitLiveMsg()
				}
			}
		}
		m.messages = append(m.messages, msg.Msg)
		m.commitFlushed()
		m.partial = ""
		flushCmd := m.flush()
		return m, tea.Batch(flushCmd, m.waitLiveMsg())

	case warningMsg:
		// transient notice from the loop (stream retry, watchdog stall).
		// Show the latest one; arm a fade tick to clear it. Re-arm the
		// channel pump so subsequent notices also surface.
		m.warning = msg.Text
		m.warningExpires = time.Now().Add(warningTTL)
		return m, tea.Batch(warningFadeCmd(), m.waitWarn())

	case warningFadeMsg:
		// only clear if no newer warning has bumped the expiry forward.
		if !m.warningExpires.IsZero() && !time.Now().Before(m.warningExpires) {
			m.warning = ""
			m.warningExpires = time.Time{}
		}
		return m, nil

	case loaderTickMsg:
		// Only animate while a turn or async compact is in flight. Letting the
		// tick die when we leave streaming/compacting keeps idle terminals quiet.
		if m.state != StateStreaming && !m.compacting {
			return m, nil
		}
		m.loaderFrame++
		return m, loaderTickCmd()

	case compactDoneMsg:
		m.compacting = false
		if msg.err != nil {
			m.lastErr = msg.err.Error()
			m.state = StateError
			return m, nil
		}
		m.messages = append(m.messages, types.Message{
			Role:    types.RoleAssistant,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: formatCompactDone(msg.stats)}},
		})
		return m, m.flush()

	case turnDoneMsg:
		m.cancelRun = nil
		// freeze elapsed at turn end. Guard zero turnStartedAt — late msgs
		// after Model reset shouldn't synthesize a huge duration.
		if !m.turnStartedAt.IsZero() {
			m.lastTurnDuration = time.Since(m.turnStartedAt)
			m.turnStartedAt = time.Time{}
		}
		switch {
		case errors.Is(msg.err, context.Canceled):
			// user pressed esc — clean cancel, not a failure. preserve any
			// messages the engine flushed before the cancel landed so the
			// scrollback isn't blanked, and stay idle (StateError would
			// gate the `/` palette and `@` picker behind error-recovery).
			if len(msg.result.Messages) > 0 {
				m.messages = msg.result.Messages
			}
			m.commitFlushed()
			m.partial = ""
			m.state = StateIdle
		case msg.err != nil:
			// drop any progressively-flushed prefix on error — there's no
			// final assistant message to dedupe against, just clear state.
			m.streamFlushed = ""
			m.streamFenceOpen = false
			m.pendingFlushedPrefix = ""
			m.state = StateError
			m.lastErr = msg.err.Error()
		default:
			m.messages = msg.result.Messages
			m.commitFlushed()
			m.partial = ""
			m.state = StateIdle
		}
		flushCmd := m.flush()
		// kick off the top-bar cost flash when a fresh event landed. Diff
		// the call-count against the previous turn so multi-iteration loops
		// fold all their per-iteration events into one visible delta.
		costCmd := m.maybeStartCostFlash()
		// drain one queued follow-up per turn so the TUI stays responsive
		// between fires. Only when last turn didn't error.
		if msg.err == nil && len(m.queue) > 0 && m.eng != nil {
			nxt := m.queue[0]
			m.queue = m.queue[1:]
			nm, runCmd := m.submit(nxt)
			return nm, tea.Batch(flushCmd, costCmd, runCmd)
		}
		return m, tea.Batch(flushCmd, costCmd)

	case costTickMsg:
		if m.costFlashFrame >= m.costFlashUntil {
			m.costFlashUntil = 0
			return m, nil
		}
		m.costFlashFrame++
		return m, costTickCmd()

	case introTickMsg:
		if !m.introActive {
			return m, nil
		}
		// build frames on first tick when width is finally known. If width
		// still hasn't arrived (initial WindowSizeMsg pending), just rearm.
		if m.introFrames == nil {
			if m.width <= 0 {
				return m, introTickCmd()
			}
			m.introFrames = introFrames(m.introStyle, m.width)
			if len(m.introFrames) == 0 {
				m.introActive = false
				return m, nil
			}
		}
		m.introIdx++
		if m.introIdx >= len(m.introFrames) {
			m.introActive = false
			m.introFrames = nil
			return m, nil
		}
		return m, introTickCmd()

	case openPaletteMsg:
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

	case PaletteSelectMsg:
		// commands AND skills both submit immediately via "/name" — runSlash
		// dispatches to the command registry first, then falls through to the
		// skill registry. unified path keeps "/calc" and "#calc → enter"
		// behaving the same.
		m.input.SetValue("/" + msg.Name)
		return m.handleSubmit()

	case PaletteDismissedMsg:
		// clear the slash-query staged in the input on esc — the user
		// cancelled the palette, no reason to leave "/foo" behind.
		if strings.HasPrefix(m.input.Value(), "/") {
			m.input.Reset()
		}
		return m, nil

	case AtPickerSelectMsg:
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

	case AtPickerDismissedMsg:
		return m, nil

	case HistorySelectMsg:
		// paste into the main input; user can edit then submit.
		m.input.SetValue(msg.Text)
		m.input.CursorEnd()
		return m, nil

	case HistoryDismissedMsg:
		return m, nil

	case openTreeMsg:
		if m.tree != nil {
			m.tree.LoadMessages(m.messages, m.currentLeafID())
			newT, cmd := m.tree.Update(ToggleSessionTreeMsg{})
			m.tree = newT
			return m, cmd
		}
		return m, nil

	case openResumeMsg:
		if m.resume != nil {
			newR, cmd := m.resume.Update(ToggleResumePickerMsg{})
			m.resume = newR
			return m, cmd
		}
		return m, nil

	case ResumeSelectMsg:
		if err := m.side().OpenSession(msg.ID); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case ResumeDismissedMsg:
		return m, nil

	case SessionForkMsg:
		if err := m.side().ForkSession(msg.FromID); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case SessionCloneMsg:
		if err := m.side().CloneSession(); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case SessionSwitchMsg:
		// F5 scope: in-place leaf switching needs deeper rollout refactor.
		// Keep the cursor selection client-side; user can /fork to materialize.
		return m, nil

	case openCostMsg:
		if m.isLocalProvider() {
			m.lastErr = "cost monitor hidden for local provider"
			return m, nil
		}
		if m.costPane == nil {
			m.costPane = NewCostPane(m.costs)
		}
		newC, cmd := m.costPane.Update(ToggleCostPaneMsg{})
		m.costPane = newC
		return m, cmd

	case openLoginMsg:
		if m.loginPane == nil {
			m.loginPane = NewLoginPane(m.side())
		}
		newL, cmd := m.loginPane.Update(ToggleLoginPaneMsg{})
		m.loginPane = newL
		return m, cmd

	case openEffortMsg:
		if m.effortPane == nil {
			m.effortPane = NewEffortPane()
		}
		m.effortPane.Show(m.thinking)
		return m, nil

	case openSettingsMsg:
		if m.settingsPane == nil {
			m.settingsPane = NewSettingsPane()
		}
		m.settingsPane.Show(SettingsSnapshot{
			Verbose:         m.verbose,
			ShowThoughts:    m.showThoughts,
			ShowNudges:      m.showNudges,
			Compact:         m.compact,
			ShowContextBar:  m.showContextBar,
			Highlight:       m.highlight,
			ShellBangSilent: m.shellBangSilent,
			ShowBee:         m.showBee,
			ShowContextPct:  m.showContextPct,
			ShowModel:       m.showModel,
			ShowCwd:         m.showCwd,
			ShowEffort:      m.showEffort,
			ShowTurnTimer:   m.showTurnTimer,
			ShowGitBranch:   m.showGitBranch,
			ShowTotalTokens: m.showTotalTokens,
		})
		return m, nil

	case settingsToggleMsg:
		// each toggle applies live + persists; side handles all three.
		var err error
		switch msg.key {
		case "verbose":
			err = m.side().SetVerbose(msg.value)
		case "show_thoughts":
			err = m.side().SetShowThoughts(msg.value)
		case "show_nudges":
			err = m.side().SetShowNudges(msg.value)
		case "compact":
			err = m.side().SetCompact(msg.value)
		case "show_context_bar":
			err = m.side().SetShowContextBar(msg.value)
		case "highlight":
			err = m.side().SetHighlight(msg.value)
		case "shell_bang_silent":
			err = m.side().SetShellBangSilent(msg.value)
		case "show_bee":
			err = m.side().SetShowBee(msg.value)
		case "show_context_pct":
			err = m.side().SetShowContextPct(msg.value)
		case "show_model":
			err = m.side().SetShowModel(msg.value)
		case "show_cwd":
			err = m.side().SetShowCwd(msg.value)
		case "show_effort":
			err = m.side().SetShowEffort(msg.value)
		case "show_turn_timer":
			err = m.side().SetShowTurnTimer(msg.value)
		case "show_git_branch":
			err = m.side().SetShowGitBranch(msg.value)
		case "show_total_tokens":
			err = m.side().SetShowTotalTokens(msg.value)
		}
		if err != nil && m.state != StateStreaming {
			// don't kill an in-flight turn over a persist hiccup; surface the
			// error only when idle.
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case openToolsMsg:
		if m.toolsPane == nil {
			m.toolsPane = NewToolsPane()
		}
		m.toolsPane.Show(m.side().ListTools())
		return m, nil

	case toolsToggleMsg:
		if err := m.side().SetToolDisabled(msg.name, msg.disabled); err != nil && m.state != StateStreaming {
			m.lastErr = err.Error()
			m.state = StateError
		}
		return m, nil

	case loginActionDoneMsg:
		// async login/logout finished — let the pane absorb it even if a
		// key in the meantime closed the pane (so busy flag clears).
		if m.loginPane != nil {
			newL, cmd := m.loginPane.Update(msg)
			m.loginPane = newL
			return m, cmd
		}
		return m, nil

	case openProviderMsg:
		if m.picker == nil {
			return m, nil
		}
		// resize to current frame so columns aren't 0-width on first open
		if m.width > 0 && m.height > 0 {
			m.picker.SetSize(m.width-4, m.height-4)
		}
		return m, m.picker.Show()

	case PickedMsg:
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

	case PickerDismissedMsg:
		return m, nil

	case PickerLoginRequestedMsg:
		// picker hit an auth error and user pressed ctrl+l. Open the login
		// pane scoped to the failing provider so they can paste a key inline.
		if m.loginPane != nil {
			m.loginPane.Show()
			m.loginPane.SelectProvider(msg.Provider)
		}
		return m, nil

	case effortPickedMsg:
		v := string(msg)
		if err := m.side().SetThinking(v); err != nil {
			m.lastErr = err.Error()
			m.state = StateError
			return m, nil
		}
		m.thinking = v
		m.effortPane.SetCurrent(v)
		return m, nil

	case openWorkspaceMsg:
		// slice 3B handles workspace; still a no-op.
		return m, nil
	case openHiveMsg:
		if m.agentView != nil {
			m.agentView.Open()
			return m, m.agentView.Init()
		}
		return m, nil
	case CloseAgentViewMsg:
		if m.agentView != nil {
			m.agentView.Close()
		}
		return m, nil
	case AttachSessionMsg:
		// attach defers to the side adapter; AgentView returns the id and the
		// outer app routes it to /attach. For now: just close the pane.
		if m.agentView != nil {
			m.agentView.Close()
		}
		return m, nil
	case agentTickMsg:
		if m.agentView != nil && m.agentView.IsOpen() {
			var cmd tea.Cmd
			m.agentView, cmd = m.agentView.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
