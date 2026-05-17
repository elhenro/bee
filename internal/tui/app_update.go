package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

	if nm, cmd, claimed := m.claimByPane(msg); claimed {
		return nm, cmd
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
		return m.onStreamDelta(msg)
	case liveMsgMsg:
		return m.onLiveMsg(msg)
	case warningMsg:
		return m.onWarning(msg)
	case warningFadeMsg:
		return m.onWarningFade(msg)
	case loaderTickMsg:
		return m.onLoaderTick(msg)
	case compactDoneMsg:
		return m.onCompactDone(msg)
	case turnDoneMsg:
		return m.onTurnDone(msg)
	case costTickMsg:
		return m.onCostTick(msg)
	case introTickMsg:
		return m.onIntroTick(msg)

	case openPaletteMsg:
		return m.onOpenPalette(msg)
	case PaletteSelectMsg:
		return m.onPaletteSelect(msg)
	case PaletteDismissedMsg:
		return m.onPaletteDismissed(msg)
	case AtPickerSelectMsg:
		return m.onAtPickerSelect(msg)
	case AtPickerDismissedMsg:
		return m, nil
	case HistorySelectMsg:
		return m.onHistorySelect(msg)
	case HistoryDismissedMsg:
		return m, nil

	case openTreeMsg:
		return m.onOpenTree(msg)
	case openResumeMsg:
		return m.onOpenResume(msg)
	case ResumeSelectMsg:
		return m.onResumeSelect(msg)
	case ResumeDismissedMsg:
		return m, nil
	case SessionForkMsg:
		return m.onSessionFork(msg)
	case SessionCloneMsg:
		return m.onSessionClone(msg)
	case SessionSwitchMsg:
		// F5 scope: in-place leaf switching needs deeper rollout refactor.
		// Keep the cursor selection client-side; user can /fork to materialize.
		return m, nil

	case openCostMsg:
		return m.onOpenCost(msg)
	case openLoginMsg:
		return m.onOpenLogin(msg)
	case openEffortMsg:
		return m.onOpenEffort(msg)
	case openSettingsMsg:
		return m.onOpenSettings(msg)
	case settingsToggleMsg:
		return m.onSettingsToggle(msg)
	case openToolsMsg:
		return m.onOpenTools(msg)
	case toolsToggleMsg:
		return m.onToolsToggle(msg)
	case loginActionDoneMsg:
		return m.onLoginActionDone(msg)

	case openProviderMsg:
		return m.onOpenProvider(msg)
	case PickedMsg:
		return m.onPicked(msg)
	case PickerDismissedMsg:
		return m, nil
	case PickerLoginRequestedMsg:
		return m.onPickerLoginRequested(msg)
	case effortPickedMsg:
		return m.onEffortPicked(msg)

	case openWorkspaceMsg:
		// slice 3B handles workspace; still a no-op.
		return m, nil
	case openHiveMsg:
		return m.onOpenHive(msg)
	case CloseAgentViewMsg:
		return m.onCloseAgentView(msg)
	case AttachSessionMsg:
		return m.onAttachSession(msg)
	case agentTickMsg:
		return m.onAgentTick(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
