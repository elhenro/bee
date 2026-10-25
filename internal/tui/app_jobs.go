package tui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

// jobsWakeMsg says a background job finished. The job itself stays queued in
// the manager; this only prompts a look.
type jobsWakeMsg struct{}

// waitJobs blocks on the next job completion. Update re-arms it on receipt,
// the same pump shape as waitWarn.
func (m Model) waitJobs() tea.Cmd {
	if m.eng == nil || m.eng.Jobs == nil {
		return nil
	}
	ch := m.eng.Jobs.Wake()
	ctx := m.baseCtx()
	return func() tea.Msg {
		select {
		case <-ch:
			return jobsWakeMsg{}
		case <-ctx.Done():
			return nil
		}
	}
}

func (m Model) onJobsWake(_ jobsWakeMsg) (tea.Model, tea.Cmd) {
	next := m.waitJobs()
	m, cmd := m.startJobTurn()
	return m, tea.Batch(next, cmd)
}

// startJobTurn starts a turn for finished background jobs, but only while bee
// is idle. A running turn needs nothing: the engine folds the result in at its
// next iteration, and onTurnDone calls this again if one landed too late. After
// an error or esc the result waits and rides along with the next turn.
func (m Model) startJobTurn() (Model, tea.Cmd) {
	if m.eng == nil || m.eng.Jobs == nil || m.eng.Jobs.Pending() == 0 ||
		m.state != StateIdle || m.compacting || m.tutorial.active || m.role == "queen" {
		return m, nil
	}
	text, display, ok := m.eng.JobNotice()
	if !ok {
		return m, nil
	}
	nm, cmd := m.submitWithDisplay(text, display)
	return nm.(Model), cmd
}

// footer chip while background jobs run
func drawStatusJobs(m Model) string {
	if m.eng == nil || m.eng.Jobs == nil {
		return ""
	}
	n := m.eng.Jobs.Running()
	if n == 0 {
		return ""
	}
	label := "1 job"
	if n > 1 {
		label = strconv.Itoa(n) + " jobs"
	}
	return m.styles.Dim.Render(glyphJob + " " + label)
}
