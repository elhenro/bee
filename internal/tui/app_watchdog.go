package tui

import (
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/types"
)

// resumeErrorDelay is the visible cancel window before a recoverable turn error
// auto-resumes. Long enough for the user to read the notice and esc / type to
// abort, short enough not to feel stuck.
const resumeErrorDelay = 3 * time.Second

// resumeContinueText is the stall-path continuation when the turn already
// produced output — continue rather than re-send the whole instruction.
const resumeContinueText = "Continue from where you left off — finish the task or give a short final answer."

// resumeAfterErrorMsg fires resumeErrorDelay after a recoverable turn error.
// gen is the resumeErrGen snapshot; a newer submit bumps resumeErrGen so the
// firing tick drops itself (the user already moved on).
type resumeAfterErrorMsg struct {
	gen          int
	continuation string
	reason       string
}

// scheduleErrorResume arms a resumeAfterErrorMsg carrying the continuation to
// re-trigger with once the delay elapses.
func scheduleErrorResume(gen int, continuation, reason string) tea.Cmd {
	return tea.Tick(resumeErrorDelay, func(time.Time) tea.Msg {
		return resumeAfterErrorMsg{gen: gen, continuation: continuation, reason: reason}
	})
}

// noteActivity records that the in-flight turn produced something — resets the
// stall clock, and the first activity after a resume clears the resume counter
// (the "genuine progress" signal so each real task gets a fresh budget).
func (m *Model) noteActivity() {
	m.lastActivityAt = time.Now()
	m.turnSawActivity = true // first sign of life lifts first-token grace
	if m.awaitingProgress {
		m.resumeCount = 0
		m.awaitingProgress = false
	}
}

// WithWatchdog mirrors the resolved cfg.Watchdog values onto the model.
func (m Model) WithWatchdog(enabled bool, stall time.Duration, maxResumes int) Model {
	m.watchdogEnabled = enabled
	m.watchdogStall = stall
	m.watchdogMaxResumes = maxResumes
	return m
}

// lastUserText returns the text of the most recent non-ephemeral user message —
// the instruction to re-send on a stall/error resume. Ephemeral UI echoes
// (queue/steer/auto-resume notices) are skipped. Empty when none.
func lastUserText(msgs []types.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != types.RoleUser || msgs[i].Ephemeral {
			continue
		}
		var b strings.Builder
		for _, blk := range msgs[i].Content {
			if blk.Type == types.BlockText {
				b.WriteString(blk.Text)
			}
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			return s
		}
	}
	return ""
}

// watchdogArmedForError reports whether a finished-turn error should schedule an
// auto-resume: watchdog on, under the per-task cap, the user isn't mid-steer,
// there's an instruction to resume, and the error is classified resumable.
func (m Model) watchdogArmedForError(err error) bool {
	if !m.watchdogEnabled || m.watchdogDisabled {
		return false
	}
	if m.resumeCount >= m.watchdogMaxResumes {
		return false
	}
	if strings.TrimSpace(m.input.Value()) != "" {
		return false
	}
	if lastUserText(m.messages) == "" {
		return false
	}
	return loop.ClassifyResume(err, loop.RunResult{}).Resume
}

// onResumeAfterError fires after the visible delay. Drops itself if superseded
// (gen bumped), if the user recovered manually (state left StateError), or if
// the user started typing. Otherwise clears the error gate and re-triggers.
func (m Model) onResumeAfterError(msg resumeAfterErrorMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.resumeErrGen || m.state != StateError || strings.TrimSpace(m.input.Value()) != "" {
		return m, nil
	}
	m.state = StateIdle
	m.lastErr = ""
	return m.retrigger(msg.continuation, msg.reason)
}

// handleWatchdog implements /watchdog [on|off] — a session-only toggle of the
// auto-resume safety net. No args flips the current state.
func (m Model) handleWatchdog(args []string) (tea.Model, tea.Cmd) {
	switch strings.ToLower(strings.TrimSpace(strings.Join(args, " "))) {
	case "on", "enable":
		m.watchdogDisabled = false
	case "off", "disable":
		m.watchdogDisabled = true
		m.stallResumePending = false
	case "":
		m.watchdogDisabled = !m.watchdogDisabled
	default:
		m.lastErr = "/watchdog: use on|off"
		m.state = StateError
		return m, nil
	}
	state := "on"
	if m.watchdogDisabled || !m.watchdogEnabled {
		state = "off"
	}
	m.messages = append(m.messages, types.Message{
		Role:      types.RoleAssistant,
		Content:   []types.ContentBlock{{Type: types.BlockText, Text: "(watchdog " + state + ")"}},
		Ephemeral: true,
	})
	return m, m.flush()
}

// retrigger re-runs the model with text as a fresh turn, bounded by
// watchdogMaxResumes. At the cap (or with nothing to resume) it parks in
// StateError so the user can take over with Enter.
func (m Model) retrigger(text, reason string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(text) == "" || m.resumeCount >= m.watchdogMaxResumes {
		m.state = StateError
		m.lastErr = "watchdog: gave up after " + strconv.Itoa(m.resumeCount) +
			" auto-resumes — press Enter to continue"
		return m, nil
	}
	m.resumeCount++
	m.awaitingProgress = true
	m.warning = "bee " + reason + " — auto-resuming (" +
		strconv.Itoa(m.resumeCount) + "/" + strconv.Itoa(m.watchdogMaxResumes) + ")…"
	m.warningExpires = time.Now().Add(warningTTL)
	nm, cmd := m.submitWithDisplay(text, "(auto-resume: "+reason+")")
	return nm, tea.Batch(cmd, warningFadeCmd())
}
