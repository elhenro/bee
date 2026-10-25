package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// route dispatches a message that no gate or open pane claimed to its handler.
// Anything unknown falls through to the input textarea.
func (m Model) route(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// re-size text input to fit
		m.input.SetWidth(max(0, msg.Width-2))
		prev := m.stream
		m.stream = NewStreamRenderer(*m.styles, max(minRenderWidth, msg.Width))
		if prev != nil {
			// a resize mid-tool-call must not orphan the call's result
			m.stream.toolUses, m.stream.afterTool = prev.toolUses, prev.afterTool
		}
		m.syncStream()
		m.palette.SetWidth(msg.Width)
		m.atpicker.SetWidth(msg.Width)
		m.history.SetWidth(msg.Width)
		m.askModel.SetWidth(msg.Width)
		m.approval.SetWidth(msg.Width)
		m.escalate.SetWidth(msg.Width)
		m.planmode.SetWidth(msg.Width)
		if m.picker != nil {
			m.picker.SetSize(msg.Width-4, msg.Height-4)
		}
		return m, nil

	case tea.KeyMsg:
		// a word that lands in one read ("up", "enter", "tab") spells a key's
		// name, so bindings would fire on it. feed it a rune at a time, as if
		// keyed in. bracketed pastes stay whole
		if len(msg.Runes) > 1 && msg.Type == tea.KeyRunes && !msg.Paste && !msg.Alt {
			return m.typeRunes(msg.Runes)
		}
		nm, cmd := m.handleKey(msg)
		// palette filter mirrors the main input. Close palette if the user
		// backspaced past the leading "/".
		if mm, ok := nm.(Model); ok && mm.palette.Active {
			val := mm.input.Value()
			if strings.HasPrefix(val, "/") {
				mm.palette.SetRunOnly(mm.state == StateStreaming)
				mm.palette.SetFilter(val[1:])
			} else {
				mm.palette.Active = false
			}
			return mm, cmd
		}
		return nm, cmd

	case inlineShellDoneMsg:
		return m.onInlineShellDone(msg)
	case streamDeltaMsg:
		return m.onStreamDelta(msg)
	case thinkDeltaMsg:
		return m.onThinkDelta(msg)
	case progressMsg:
		return m.onProgress(msg)
	case liveMsgMsg:
		return m.onLiveMsg(msg)
	case toolTailMsg:
		return m.onToolTail(msg)
	case warningMsg:
		return m.onWarning(msg)
	case warningFadeMsg:
		return m.onWarningFade(msg)
	case jobsWakeMsg:
		return m.onJobsWake(msg)
	case loaderTickMsg:
		return m.onLoaderTick(msg)
	case glowTickMsg:
		return m.onGlowTick(msg)
	case compactDoneMsg:
		return m.onCompactDone(msg)
	case handoffReadyMsg:
		return m.onHandoffReady(msg)
	case externalDoneMsg:
		return m.onExternalDone(msg)
	case turnDoneMsg:
		return m.onTurnDone(msg)
	case resumeAfterErrorMsg:
		return m.onResumeAfterError(msg)
	case goalEvalDoneMsg:
		return m.onGoalEvalDone(msg)
	case recapIdleTickMsg:
		return m.onRecapIdleTick(msg)
	case recapReadyMsg:
		return m.onRecapReady(msg)
	case changeMapMsg:
		return m.onChangeMap(msg)
	case costTickMsg:
		return m.onCostTick(msg)
	case introTickMsg:
		return m.onIntroTick(msg)
	case tutorialTickMsg:
		return m.onTutorialTick(msg)
	case openTutorialMsg:
		return m.onOpenTutorial(msg)

	case openPaletteMsg:
		return m.onOpenPalette(msg)
	case PaletteSelectMsg:
		return m.onPaletteSelect(msg)
	case PaletteDismissedMsg:
		return m.onPaletteDismissed(msg)
	case AtPickerSelectMsg:
		return m.onAtPickerSelect(msg)
	case HistorySelectMsg:
		return m.onHistorySelect(msg)

	case openTreeMsg:
		return m.onOpenTree(msg)
	case openResumeMsg:
		return m.onOpenResume(msg)
	case ResumeSelectMsg:
		return m.onResumeSelect(msg)
	case openRewindMsg:
		return m.onOpenRewind(msg)
	case RewindSelectMsg:
		return m.onRewindSelect(msg)
	case checkpointDoneMsg:
		return m.onCheckpointDone(msg)
	case SessionForkMsg:
		return m.onSessionFork(msg)
	case SessionCloneMsg:
		return m.onSessionClone(msg)
	// dismissals change no state. SessionSwitchMsg: F5 scope, in-place leaf
	// switching needs deeper rollout refactor; keep the cursor selection
	// client-side, user can /fork to materialize.
	case AtPickerDismissedMsg, HistoryDismissedMsg, ResumeDismissedMsg, RewindDismissedMsg, SessionSwitchMsg:
		return m, nil

	case openCostMsg:
		return m.onOpenCost(msg)
	case openUsageMsg:
		return m.onOpenUsage(msg)
	case openLoginMsg:
		return m.onOpenLogin(msg)
	case openRoleMsg:
		return m.onOpenRole(msg)
	case openEffortMsg:
		return m.onOpenEffort(msg)
	case openSettingsMsg:
		return m.onOpenSettings(msg)
	case settingsToggleMsg:
		return m.onSettingsToggle(msg)
	case settingsSetIntMsg:
		return m.onSettingsSetInt(msg)
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
		// cancelling the picker mid-/handoff disarms the flow; the stuck turn
		// (if any) keeps running since we never reached onHandoffPicked.
		m.handoffActive = false
		return m, nil
	case PickerLoginRequestedMsg:
		return m.onPickerLoginRequested(msg)
	case rolePickedMsg:
		return m.onRolePicked(msg)
	case effortPickedMsg:
		return m.onEffortPicked(msg)

	case updateAvailableMsg:
		return m.onUpdateAvailable(msg)
	case updateDecisionMsg:
		return m.onUpdateDecision(msg)
	case updateAppliedMsg:
		return m.onUpdateApplied(msg)

	case openHiveMsg:
		return m.onOpenHive(msg)
	// attach defers to the side adapter; AgentView returns the id and the
	// outer app routes it to /attach. For now: just close the pane.
	case CloseAgentViewMsg, AttachSessionMsg:
		return m.onCloseAgentView()
	case agentTickMsg:
		return m.onAgentTick(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
