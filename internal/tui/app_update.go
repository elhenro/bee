package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Update is the bubbletea main switch.
func (m Model) Update(msg tea.Msg) (resultModel tea.Model, resultCmd tea.Cmd) {
	// the agent view opens from a key or a slash command, neither of which can
	// hand back a cmd: arm its refresh chain on the way out, whichever path won.
	// only the pointer is captured so the deferred closure does not drag the
	// Model along.
	av := m.agentView
	defer func() {
		if start := av.start(); start != nil {
			resultCmd = tea.Batch(resultCmd, start)
		}
	}()
	// streaming deltas and ticks arrive far more often than keys and never
	// touch the input: skip the textarea height dance and its second Model copy.
	if touchesInput(msg) {
		// pre-grow textarea so any input mutation in this turn (handleKey,
		// SetValue from a palette/picker, etc.) wraps inside a tall viewport
		// instead of scrolling YOffset down and hiding row 0.
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
	}
	// global hard-quit gate — runs above every pane/modal so the user is
	// never trapped. ctrl+c quits immediately (POSIX cancel convention);
	// ctrl+d requires two presses within quitConfirmWindow. Any other key
	// disarms the confirm so a stray ctrl+d in the input bar doesn't leave
	// the program one-keystroke from death.
	if km, ok := msg.(tea.KeyMsg); ok {
		switch k := km.String(); k {
		case "ctrl+c", "ctrl+d":
			if k == "ctrl+c" || (m.quitArmed && time.Since(m.quitArmedAt) <= quitConfirmWindow) {
				if m.cancelRun != nil {
					m.cancelRun()
				}
				return m, tea.Quit
			}
			m.quitArmed, m.quitArmedAt = true, time.Now()
			return m, nil
		default:
			m.quitArmed = false
		}
	}
	// Seeded auto-submit from Init (skill dispatch into the TUI). Route it
	// through the normal submit path once; clear the seed so it can't refire.
	if _, ok := msg.(initFlushMsg); ok {
		return m, m.flush()
	}
	if as, ok := msg.(autoSubmitMsg); ok {
		m.seedPrompt = ""
		m.input.SetValue(as.text)
		return m.handleSubmit()
	}
	// Dangerous-command prompt arrives from the engine goroutine via the
	// Approver adapter. Surface the modal so the user can pick.
	if ask, ok := msg.(ApprovalAskMsg); ok {
		// yolo: auto-approve flagged commands, no modal. reads the live toggle so
		// alt+y takes effect mid-session. hardline refusals still fire upstream
		// in the shell tool — yolo only answers approvable prompts.
		if m.yolo {
			if m.approver != nil {
				m.approver.Resolve(ask.UseID, ApprovalAllow)
			}
			return m, nil
		}
		m.approval.Show(ApprovalRequest{
			ToolName: "bash",
			Action:   ask.Cmd,
			Reason:   ask.Reason,
			Key:      ask.Key,
			UseID:    ask.UseID,
		})
		return m, nil
	}
	// Decision arrives async from the cmd produced by ApprovalModel.decide,
	// at which point the modal has already deactivated itself. Handle it
	// outside the Active gate so the engine unblocks. Without this, the
	// approver Request() never resolves and the tool call hangs forever.
	if dec, ok := msg.(ApprovalDecisionMsg); ok {
		// turn is still in flight: the engine runs the approved tool and keeps
		// streaming. Leave m.state as-is (StateStreaming) so the loader tick
		// keeps re-arming and the input stays locked until turnDoneMsg.
		if m.approver != nil {
			m.approver.Resolve(dec.UseID, dec.Decision)
		}
		// the approval wait was a human block, not a model stall. the loader
		// tick chain keeps running while the modal is up, so no revive.
		m.lastActivityAt = time.Now()
		return m, nil
	}
	// ask_user question arrives from the engine goroutine via the Asker
	// adapter. Surface the picker.
	if a, ok := msg.(AskShowMsg); ok {
		m.askModel.Show(a.UseID, a.Question)
		return m, nil
	}
	// Answer arrives async from the cmd produced by AskModel.answer; the picker
	// has already deactivated itself. Resolve outside the Active gate so the
	// blocked tool unblocks.
	if a, ok := msg.(AskAnswerMsg); ok {
		if m.asker != nil {
			m.asker.Resolve(a.UseID, a.Answer)
		}
		// the question wait was a human block, not a model stall.
		m.lastActivityAt = time.Now()
		return m, nil
	}
	// escalate pick: submit the chosen option as the next user turn.
	if c, ok := msg.(EscalateChoiceMsg); ok {
		m.input.SetValue(c.Text)
		return m.handleSubmit()
	}
	// post-plan pick: switch mode (maybe clear context), then proceed.
	if c, ok := msg.(PlanProceedMsg); ok {
		return m.onPlanProceed(c)
	}
	// modals consume input only. Everything else (turnDone, stream deltas,
	// ticks, window size, pump re-arms) must reach the main switch or the
	// pumps die and a turn never completes while a modal is up.
	if isInputMsg(msg) {
		if cmd, ok := claimModal(&m.escalate, msg); ok {
			return m, cmd
		}
		if cmd, ok := claimModal(&m.planmode, msg); ok {
			return m, cmd
		}
		if cmd, ok := claimModal(&m.approval, msg); ok {
			return m, cmd
		}
		if cmd, ok := claimModal(&m.askModel, msg); ok {
			return m, cmd
		}
		if cmd, ok := claimModal(&m.updatePrompt, msg); ok {
			return m, cmd
		}
	}
	// a human-blocked wait is not a model stall: keep the watchdog clock fresh
	// while a prompt that blocks the engine is up.
	if _, ok := msg.(loaderTickMsg); ok && (m.approval.Active || m.askModel.Active) {
		m.lastActivityAt = time.Now()
	}
	// tutorial claims keys while active; ticks/window-size fall through.
	if m.tutorial.active {
		if km, ok := msg.(tea.KeyMsg); ok {
			return m.onTutorialKey(km)
		}
	}

	if nm, cmd, claimed := m.claimByPane(msg); claimed {
		return nm, cmd
	}
	return m.route(msg)
}

// touchesInput reports whether handling msg can change the input text or its
// width. Unknown messages count as input: the fallthrough feeds them to the
// textarea.
func touchesInput(msg tea.Msg) bool {
	switch msg.(type) {
	case streamDeltaMsg, thinkDeltaMsg, progressMsg, toolTailMsg, liveMsgMsg,
		warningFadeMsg, loaderTickMsg, glowTickMsg, costTickMsg, introTickMsg,
		tutorialTickMsg, agentTickMsg, agentRowsMsg, atIndexMsg:
		return false
	}
	return true
}

// modal is an inline prompt that consumes input while active.
type modal[T any] interface {
	isActive() bool
	Update(tea.Msg) (T, tea.Cmd)
}

func (p UpdatePrompt) isActive() bool { return p.Active }

// claimModal feeds msg to the modal at *p when it is active.
func claimModal[T modal[T]](p *T, msg tea.Msg) (cmd tea.Cmd, claimed bool) {
	if !(*p).isActive() {
		return nil, false
	}
	*p, cmd = (*p).Update(msg)
	return cmd, true
}

// isInputMsg reports whether msg is user input a modal may claim.
func isInputMsg(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		return true
	}
	return false
}
