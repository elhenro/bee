package tui

import (
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/loop"
)

var errEngineBusy = errors.New("bee is running, retry when the turn finishes")

// engineBusy reports whether an engine goroutine may still be touching the
// engine: a streaming turn, a compaction, or a cancelled run that has not
// returned yet (esc flips state to idle before the goroutine unwinds).
func (m Model) engineBusy() bool {
	if m.state == StateStreaming || m.compacting {
		return true
	}
	if m.runDone != nil {
		select {
		case <-m.runDone:
		default:
			return true
		}
	}
	return false
}

// cfgSync is the live role/thinking/yolo toggle state. Mid-turn changes only
// touch the Model; the snapshot is applied to the engine config right before
// the next run, once the previous goroutine has released the engine.
type cfgSync struct {
	role, thinking string
	yolo           bool
}

func (m Model) cfgSnapshot() cfgSync {
	return cfgSync{role: m.role, thinking: m.thinking, yolo: m.yolo}
}

func (c cfgSync) apply(e *loop.Engine) {
	if e == nil {
		return
	}
	e.Cfg.Role = c.role
	e.Cfg.Thinking = c.thinking
	e.Cfg.Yolo = c.yolo
}

// mirrorToEngine writes the toggles into the engine config now when the
// engine is idle; while busy they wait for the next submit.
func (m *Model) mirrorToEngine() {
	if m.eng == nil || m.engineBusy() {
		return
	}
	m.cfgSnapshot().apply(m.eng)
}

// fail surfaces msg: a transient notice while a turn streams (StateError
// would unlock the input and orphan the running turn), the error line
// otherwise.
func (m Model) fail(msg string) (Model, tea.Cmd) {
	if m.state == StateStreaming {
		m.warning = msg
		m.warningExpires = time.Now().Add(warningTTL)
		return m, warningFadeCmd()
	}
	m.lastErr = msg
	m.state = StateError
	return m, nil
}

// cancelTurn aborts the in-flight turn on user request (esc, /stop). It also
// drops any pending watchdog resume: a stall-cancel already nil'd cancelRun,
// and a surviving stallResumePending would auto-resume the turn the user just
// stopped when its cancelled result lands.
func (m *Model) cancelTurn() {
	if m.cancelRun != nil {
		m.cancelRun()
		m.cancelRun = nil
	}
	m.state = StateIdle
	m.stallResumePending = false
	m.stallResumeText, m.stallResumeReason = "", ""
	m.resumeCount = 0
	m.awaitingProgress = false
}
