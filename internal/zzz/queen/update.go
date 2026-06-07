package queen

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/zzz"
)

const tickInterval = 600 * time.Millisecond

func (m *Model) Init() tea.Cmd { return tea.Batch(tickCmd(), m.waitMsg()) }

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) waitMsg() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.msgs
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(v)
	case tickMsg:
		m.tick++
		return m, tickCmd()
	case wIterMsg:
		w := m.workers[v.idx]
		w.iter, w.maxIt = v.n, v.max
		return m, m.waitMsg()
	case wPhaseMsg:
		m.workers[v.idx].phase = v.p
		return m, m.waitMsg()
	case wTokensMsg:
		m.workers[v.idx].tokens = v.t
		return m, m.waitMsg()
	case wCommitMsg:
		m.workers[v.idx].commits++
		return m, m.waitMsg()
	case wLogMsg:
		m.workers[v.idx].last = truncate(v.text, 80)
		return m, m.waitMsg()
	case wDoneMsg:
		w := m.workers[v.idx]
		w.status = terminalStatus(v.run, v.err)
		w.phase = "done"
		m.nDone++
		return m, m.waitMsg()
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q":
		if m.allDone() {
			return m, tea.Quit
		}
	case "ctrl+d":
		// always exits; canceling ctx aborts any wedged worker iteration.
		m.cancelAll()
		return m, tea.Quit
	case "ctrl+c":
		if m.allDone() {
			return m, tea.Quit
		}
		// first ctrl+c = graceful stop-all (each worker finishes its current
		// iter); second = hard cancel via ctx so stuck iterations die too.
		if m.stopArmed {
			m.cancelAll()
			return m, tea.Quit
		}
		m.stopArmed = true
		m.broadcast(zzz.Steer{Kind: zzz.SteerStop})
		return m, nil
	}
	return m, nil
}

func terminalStatus(r *zzz.Run, err error) string {
	if err != nil {
		return "failed"
	}
	if r != nil && r.Status != "" {
		return r.Status
	}
	return "done"
}
