package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/loop"
)

// onRecapIdleTick fires recapIdleDelay after a clean turn finish. Drops
// itself if a newer submit bumped m.recapGen, or if the user is no longer
// idle (state != StateIdle, or showRecap toggled off mid-wait).
func (m Model) onRecapIdleTick(msg recapIdleTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.recapGen || m.state != StateIdle || !m.showRecap {
		return m, nil
	}
	return m, m.recapCmd(msg.msgs)
}

// onChangeMap prints the review card in the same dim voice as the recap.
func (m Model) onChangeMap(msg changeMapMsg) (tea.Model, tea.Cmd) {
	if msg.text == "" {
		return m, nil
	}
	w := m.width
	if w < minRenderWidth {
		w = 80
	}
	rows := strings.Split(msg.text, "\n")
	for i, row := range rows {
		rows[i] = m.styles.Dim.Render(clipLine(row, w-2))
	}
	return m, m.scroll.print(strings.Join(rows, "\n"))
}

// dimItalic is the quiet voice of post-turn notices (recap, goal status).
var dimItalic = lipgloss.NewStyle().Foreground(fgOyster).Italic(true)

func (m Model) onRecapReady(msg recapReadyMsg) (tea.Model, tea.Cmd) {
	// Error case renders too so the toggle is observable. Skip is silent:
	// a model that declines to recap produced no useful output, no point
	// cluttering the transcript with "※ recap: (skipped)".
	var body string
	switch {
	case msg.text != "":
		body = "※ recap: " + msg.text
	case msg.err != "":
		body = "※ recap failed: " + msg.err
	default:
		return m, nil
	}
	return m, m.scroll.print(dimItalic.Render(body))
}

func (m Model) onTurnDone(msg turnDoneMsg) (tea.Model, tea.Cmd) {
	// ignore a stale result from a turn that was cancelled or superseded: its
	// late turnDoneMsg would otherwise nil cancelRun (breaking esc for the live
	// turn), flip state to idle, and clobber m.messages with an old snapshot.
	// mirrors the recapGen / resumeErrGen epoch guards.
	if msg.gen != m.turnGen {
		return m, nil
	}
	m.cancelRun = nil
	// turn finished/cancelled/errored — no tools left in flight.
	m.pendingTools = nil
	// freeze elapsed at turn end. Guard zero turnStartedAt — late msgs
	// after Model reset shouldn't synthesize a huge duration.
	if !m.turnStartedAt.IsZero() {
		m.lastTurnDuration = time.Since(m.turnStartedAt)
		m.turnStartedAt = time.Time{}
	}
	// error-resume cmd, populated in the error branch when the watchdog arms.
	var errResumeCmd tea.Cmd
	// a note for scrollback that is not part of the message list
	var note string // printed after the turn's messages, so it is queued after the flush
	// snapCmd, populated on a clean finish, snapshots the work-tree for rewind.
	var snapCmd tea.Cmd
	switch {
	case m.stallResumePending && errors.Is(msg.err, context.Canceled):
		// watchdog cancelled a stalled turn. The in-flight goroutine has now
		// returned (we got its Canceled), so it's safe to re-trigger without
		// two eng.Run calls overlapping. Carry the partial messages for context.
		m.stallResumePending = false
		text, reason := m.stallResumeText, m.stallResumeReason
		m.stallResumeText, m.stallResumeReason = "", ""
		if len(msg.result.Messages) > 0 {
			m.messages = msg.result.Messages
		}
		m.dropFlushed()
		m.partial = ""
		m.thinkPartial = ""
		return m.retrigger(text, reason)
	case errors.Is(msg.err, context.Canceled):
		// user pressed esc — clean cancel, not a failure. preserve any
		// messages the engine flushed before the cancel landed so the
		// scrollback isn't blanked, and stay idle (StateError would
		// gate the `/` palette and `@` picker behind error-recovery).
		if len(msg.result.Messages) > 0 {
			m.messages = msg.result.Messages
		}
		if m.interrupted {
			m.interrupted = false
			note = "(interrupted)"
		}
		m.commitFlushed()
		m.partial = ""
		m.thinkPartial = ""
		m.state = StateIdle
		// user intervened — give the next task a fresh resume budget.
		m.resumeCount = 0
		m.awaitingProgress = false
	case errors.Is(msg.err, loop.ErrEscalate):
		// escalate isn't a crash — the model handed control back. The yellow
		// escalate card already sits in scrollback (rendered live), so don't
		// repeat it as a red error block. Stay idle; open the option picker
		// when the model supplied discrete choices.
		m.dropFlushed()
		m.thinkPartial = ""
		m.partial = ""
		m.state = StateIdle
		var esc *loop.EscalateError
		if errors.As(msg.err, &esc) {
			m.escalate.Show(esc.Options)
		}
	case msg.err != nil:
		// drop any progressively-flushed prefix on error — there's no
		// final assistant message to dedupe against, just clear state.
		m.dropFlushed()
		m.thinkPartial = ""
		m.state = StateError
		m.lastErr = msg.err.Error()
		// recoverable error → schedule a bounded auto-resume after a short,
		// visible cancel window instead of parking until the user hits Enter.
		if m.watchdogArmedForError(msg.err) {
			d := loop.ClassifyResume(msg.err, msg.result)
			if len(msg.result.Messages) > 0 {
				m.messages = msg.result.Messages // carry context into the resume
			}
			m.resumeErrGen++
			m = m.withWarn("error — auto-resuming in 3s (esc or type to cancel)…")
			errResumeCmd = tea.Batch(scheduleErrorResume(m.resumeErrGen, d.Continuation, d.Reason), warningFadeCmd())
		}
	default:
		m.messages = msg.result.Messages
		m.commitFlushed()
		m.partial = ""
		m.thinkPartial = ""
		m.state = StateIdle
		// clean finish — reset the resume budget for the next task.
		m.resumeCount = 0
		m.awaitingProgress = false
		// snapshot the resulting code state so the user can rewind to this turn.
		snapCmd = m.snapshotAfterTurn(msg.result.Messages)
	}
	flushCmd := m.flush()
	if note != "" {
		flushCmd = tea.Batch(flushCmd, m.printNotice(note))
	}
	// kick off the top-bar cost flash when a fresh event landed. Diff
	// the call-count against the previous turn so multi-iteration loops
	// fold all their per-iteration events into one visible delta.
	costCmd := m.maybeStartCostFlash()
	// a steer the loop never got to read becomes a follow-up of its own
	m.queue = append(m.queue, drainSteer(m.eng)...)
	if msg.err != nil {
		return m, tea.Batch(flushCmd, costCmd, errResumeCmd)
	}
	cmds := []tea.Cmd{flushCmd, costCmd, snapCmd, m.changeMapCmd(msg.result.Messages)}
	// recap: optional one-line side-LLM summary, dim italic, after the
	// last assistant turn. Gated by m.showRecap so disabled = no extra
	// tokens. Only when the turn was substantive enough to warrant a recap —
	// short greetings/Q&A skip the side call entirely (saves tokens, avoids
	// noisy "(skipped)" lines from the model). Deferred recapIdleDelay
	// so a follow-up submit cancels the pending tick (gen mismatch).
	if m.showRecap && recapWorthwhile(msg.result.Messages, m.lastTurnDuration) {
		cmds = append(cmds, recapIdleCmd(m.recapGen, msg.result.Messages))
	}
	// drain one queued follow-up per turn so the TUI stays responsive
	// between fires.
	if len(m.queue) > 0 && m.eng != nil {
		nxt := m.queue[0]
		m.queue = m.queue[1:]
		nm, runCmd := m.submit(nxt)
		return nm, tea.Batch(append(cmds, runCmd)...)
	}
	// a background job finished after the loop's last look: start its turn now
	if nm, jobCmd := m.startJobTurn(); jobCmd != nil {
		return nm, tea.Batch(append(cmds, jobCmd)...)
	}
	// post-scout handoff: a clean scout turn produced a plan, but scout strips
	// mutators so "do it" silently no-ops. Offer to switch into worker
	// (optionally a fresh session carrying just the plan). Queue is empty here
	// (drained above); skip when a goal loop drives its own continuation, and
	// skip when the turn produced no text (nothing to act on or carry).
	if m.role == "scout" && !m.goal.Active {
		if plan := lastAssistantText(msg.result.Messages); plan != "" {
			m.pendingPlan = plan
			m.planmode.Show()
		}
	}
	// goal loop: on a clean finish with the queue empty, judge the active
	// goal and either announce success, stop on caps, or auto-continue. No-op
	// when no goal is active.
	m, goalCmd := m.maybeStartGoalEval()
	return m, tea.Batch(append(cmds, goalCmd)...)
}
