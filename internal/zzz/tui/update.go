package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/zzz"
)

// tickInterval drives the sleeping-bee animation + elapsed clock. 600ms is
// slow enough to read as breathing without flicker.
const tickInterval = 600 * time.Millisecond

// Init pumps the first tick + the first msg-channel read.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), m.waitMsg())
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// waitMsg pulls one event off m.msgs and surfaces it as a tea.Msg. Returns
// nil when the channel is closed so bubbletea stops pumping.
func (m *Model) waitMsg() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.msgs
		if !ok {
			return nil
		}
		return msg
	}
}

// Update handles every tea.Msg variant we emit + bubbletea built-ins.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
		m.input.SetWidth(maxInt(40, v.Width-4))
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(v)
	case tickMsg:
		m.tick++
		m.now = time.Time(v)
		return m, tickCmd()
	case iterMsg:
		m.touchIter(v.n, v.max)
		return m, m.waitMsg()
	case phaseMsg:
		m.touchPhase(string(v))
		return m, m.waitMsg()
	case tokensMsg:
		m.tokens = zzz.TokenStat(v)
		if len(m.rows) > 0 && m.rows[len(m.rows)-1].status == "running" {
			m.rows[len(m.rows)-1].tokens = m.tokens
		}
		return m, m.waitMsg()
	case commitsMsg:
		return m, m.waitMsg()
	case logMsg:
		m.log = append(m.log, v)
		if len(m.log) > 200 {
			m.log = m.log[len(m.log)-200:]
		}
		m.absorbResult(v)
		return m, m.waitMsg()
	case doneMsg:
		m.done = true
		m.final = v.run
		m.finalEr = v.err
		return m, m.waitMsg()
	}
	return m, nil
}

func (m *Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		// first ctrl+c = graceful stop; second = abort (handled by signal)
		m.pushSteer(zzz.Steer{Kind: zzz.SteerStop})
		return m, nil
	case "ctrl+d":
		if m.done {
			return m, tea.Quit
		}
	case "q":
		if m.done {
			return m, tea.Quit
		}
	case "enter":
		txt := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		if txt == "" {
			return m, nil
		}
		m.dispatchInput(txt)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// dispatchInput parses an operator line: `/stop`, `/abort`, `/note <text>`,
// or free text (treated as a note).
func (m *Model) dispatchInput(s string) {
	if !strings.HasPrefix(s, "/") {
		m.pushSteer(zzz.Steer{Kind: zzz.SteerNote, Text: s})
		return
	}
	parts := strings.SplitN(s, " ", 2)
	cmd := strings.ToLower(parts[0])
	rest := ""
	if len(parts) > 1 {
		rest = strings.TrimSpace(parts[1])
	}
	switch cmd {
	case "/stop", "/quit":
		m.pushSteer(zzz.Steer{Kind: zzz.SteerStop})
	case "/abort", "/kill":
		m.pushSteer(zzz.Steer{Kind: zzz.SteerAbort})
	case "/note", "/say", "/nudge":
		if rest == "" {
			m.log = append(m.log, logMsg{level: "warn", text: "[zzz] /note needs text"})
			return
		}
		m.pushSteer(zzz.Steer{Kind: zzz.SteerNote, Text: rest})
	case "/help", "/?":
		m.log = append(m.log, logMsg{level: "info", text: "[zzz] commands: /stop /abort /note <text> · plain text = note"})
	default:
		m.pushSteer(zzz.Steer{Kind: zzz.SteerNote, Text: s})
	}
}

func (m *Model) pushSteer(s zzz.Steer) {
	select {
	case m.steer <- s:
		switch s.Kind {
		case zzz.SteerNote:
			m.log = append(m.log, logMsg{level: "info", text: styYou.Render("you ›") + " " + s.Text})
		case zzz.SteerStop:
			m.log = append(m.log, logMsg{level: "warn", text: "[zzz] stop requested — finishing current iteration"})
		case zzz.SteerAbort:
			m.log = append(m.log, logMsg{level: "err", text: "[zzz] abort requested"})
		}
	default:
		m.log = append(m.log, logMsg{level: "warn", text: "[zzz] steering buffer full — try again"})
	}
}

// touchIter starts a new iteration row in "running" state.
func (m *Model) touchIter(n, max int) {
	m.iter, m.maxIt = n, max
	m.rows = append(m.rows, iterRow{n: n, status: "running", when: time.Now()})
	if len(m.rows) > 400 {
		m.rows = m.rows[len(m.rows)-400:]
	}
}

// touchPhase updates the live phase + maps terminal phases onto iter status.
func (m *Model) touchPhase(p string) {
	m.phase = p
	if len(m.rows) == 0 {
		return
	}
	last := &m.rows[len(m.rows)-1]
	switch p {
	case "noop":
		last.status = "noop"
	case "agent-blocked", "hard-error", "commit-fail":
		last.status = "failed"
	}
}

// absorbResult sniffs zzz.Println lines for commit subjects so we can show
// them inline. Drive emits no structured iter-result msg, only Println.
func (m *Model) absorbResult(v logMsg) {
	if len(m.rows) == 0 {
		return
	}
	// no-op for now; tokens + phase already populate the row. Subject would
	// require an explicit Println from Drive — left as a future hook.
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
