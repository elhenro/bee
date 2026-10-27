package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/agents"
	"github.com/elhenro/bee/internal/bgreg"
)

// AgentView is the multi-bee management pane (Left arrow / Ctrl+H).
// List + peek + inline reply. Refreshes every second so live bg bees update.
type AgentView struct {
	rows     []bgreg.Status
	selected int
	reply    textinput.Model
	err      string
	visible  bool
	// gen counts opens. A tick or refresh result from an earlier open carries
	// an older gen and is dropped, so close+reopen never runs two chains.
	gen int
	// starting is set by Open until start arms the refresh chain
	starting bool
}

type AttachSessionMsg struct{ ID string }
type CloseAgentViewMsg struct{}

// agentTickMsg asks the open view to refresh.
type agentTickMsg struct{ gen int }

// agentRowsMsg carries a finished refresh back to the UI goroutine.
type agentRowsMsg struct {
	gen  int
	rows []bgreg.Status
	err  error
}

const agentRefreshInterval = time.Second

func NewAgentView() *AgentView {
	ti := textinput.New()
	ti.Placeholder = "reply to selected agent…"
	ti.CharLimit = 4000
	ti.Prompt = "↵ "
	return &AgentView{reply: ti}
}

func (a *AgentView) Open() {
	a.visible = true
	a.reply.Focus()
	a.gen++
	a.starting = true
}

func (a *AgentView) Close()       { a.visible = false; a.reply.Blur() }
func (a *AgentView) IsOpen() bool { return a.visible }

// start arms the refresh chain once after Open. Nil-safe: the Model calls it
// on every Update.
func (a *AgentView) start() tea.Cmd {
	if a == nil || !a.starting {
		return nil
	}
	a.starting = false
	return a.refreshCmd()
}

// refreshCmd reads the registry off the UI goroutine: List hits the disk and
// WakeWaiting can spawn processes.
func (a *AgentView) refreshCmd() tea.Cmd {
	gen := a.gen
	return func() tea.Msg {
		statuses, err := bgreg.List()
		if err == nil {
			// a reply to an agent whose process already exited has nobody to read it
			agents.WakeWaiting(statuses)
		}
		return agentRowsMsg{gen: gen, rows: statuses, err: err}
	}
}

// tickCmd schedules the next refresh. It is armed only when a result lands,
// so a slow refresh never stacks up behind the next one.
func (a *AgentView) tickCmd() tea.Cmd {
	gen := a.gen
	return tea.Tick(agentRefreshInterval, func(time.Time) tea.Msg { return agentTickMsg{gen: gen} })
}

func (a *AgentView) applyRows(msg agentRowsMsg) {
	if msg.err != nil {
		a.err = msg.err.Error()
		return
	}
	a.err = ""
	a.rows = msg.rows
	if a.selected >= len(a.rows) {
		a.selected = 0
	}
}

func (a *AgentView) SubmitReply(text string) error {
	if a.selected < 0 || a.selected >= len(a.rows) {
		return errors.New("no agent selected")
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("empty reply")
	}
	if err := bgreg.InboxAppend(a.rows[a.selected].SessionID, text); err != nil {
		return err
	}
	a.reply.SetValue("")
	return nil
}

func (a *AgentView) Update(msg tea.Msg) (*AgentView, tea.Cmd) {
	if !a.visible {
		return a, nil
	}
	switch m := msg.(type) {
	case agentTickMsg:
		if m.gen != a.gen {
			return a, nil
		}
		return a, a.refreshCmd()
	case agentRowsMsg:
		if m.gen != a.gen {
			return a, nil
		}
		a.applyRows(m)
		return a, a.tickCmd()
	case tea.KeyMsg:
		switch m.String() {
		// the reply input is always focused, so letters are text: only
		// arrows and esc drive the list
		case "esc":
			return a, func() tea.Msg { return CloseAgentViewMsg{} }
		case "up":
			if a.selected > 0 {
				a.selected--
			}
			return a, nil
		case "down":
			if a.selected < len(a.rows)-1 {
				a.selected++
			}
			return a, nil
		case "enter":
			txt := strings.TrimSpace(a.reply.Value())
			if txt == "" {
				if a.selected < len(a.rows) {
					id := a.rows[a.selected].SessionID
					return a, func() tea.Msg { return AttachSessionMsg{ID: id} }
				}
				return a, nil
			}
			if err := a.SubmitReply(txt); err != nil {
				a.err = err.Error()
			}
			return a, nil
		}
	}
	var cmd tea.Cmd
	a.reply, cmd = a.reply.Update(msg)
	return a, cmd
}

var (
	avTitleStyle = lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	avSelStyle   = lipgloss.NewStyle().Foreground(accentHoney)
	avDim        = lipgloss.NewStyle().Foreground(fgSmoke)
)

func (a *AgentView) Render(width int) string {
	if !a.visible {
		return ""
	}
	var lines []string
	lines = append(lines, avTitleStyle.Render("⬢ Agents"), "")
	if len(a.rows) == 0 {
		lines = append(lines, avDim.Render("no background bees yet — spawn one with /bg <task>"))
	} else {
		for i, r := range a.rows {
			marker := " "
			if i == a.selected {
				marker = avSelStyle.Render(glyphSel)
			}
			lines = append(lines, fmt.Sprintf("%s %s  %-10s  %-8s  ⬢ %s",
				marker,
				fmt.Sprintf("%-40s", truncateRunes(r.Task, 40)),
				stateLabel(r.State),
				humanAgeShort(r.UpdatedAt),
				r.SessionID[:min(len(r.SessionID), 8)],
			))
		}
	}
	lines = append(lines, "", avDim.Render("─── peek ───"))
	if a.selected < len(a.rows) && a.rows[a.selected].LastResponse != "" {
		w := width - 2
		if w < 20 {
			w = 60
		}
		lines = append(lines, agentWrap(a.rows[a.selected].LastResponse, w))
	} else {
		lines = append(lines, avDim.Render("(no response yet)"))
	}
	lines = append(lines, "", a.reply.View())
	if a.err != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(semError).Render("error: "+a.err))
	}
	return strings.Join(lines, "\n")
}

func stateLabel(s bgreg.State) string {
	switch s {
	case bgreg.StateActive:
		return lipgloss.NewStyle().Foreground(accentHoney).Render(string(s))
	case bgreg.StateAwaiting:
		return lipgloss.NewStyle().Foreground(semWarning).Render(string(s))
	case bgreg.StateFailed:
		return lipgloss.NewStyle().Foreground(semError).Render(string(s))
	default:
		return avDim.Render(string(s))
	}
}

func humanAgeShort(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// agentWrap wraps on rune boundaries; byte indexing would split multi-byte
// runes in non-ASCII task summaries and garble the pane.
func agentWrap(s string, n int) string {
	r := []rune(s)
	if n <= 10 || len(r) <= n {
		return s
	}
	var b strings.Builder
	for len(r) > n {
		cut := n
		for cut > 0 && r[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = n
		}
		b.WriteString(string(r[:cut]))
		b.WriteByte('\n')
		for cut < len(r) && r[cut] == ' ' {
			cut++
		}
		r = r[cut:]
	}
	b.WriteString(string(r))
	return b.String()
}
