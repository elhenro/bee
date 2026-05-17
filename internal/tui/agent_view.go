package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/bgreg"
)

// AgentView is the multi-bee management pane (Left arrow / Ctrl+H).
// List + peek + inline reply. Refreshes every tick so live bg bees update.
type AgentView struct {
	rows     []AgentRow
	selected int
	reply    textinput.Model
	err      string
	visible  bool
}

type AgentRow struct {
	SessionID    string
	State        bgreg.State
	Task         string
	LastResponse string
	Updated      time.Time
}

type AttachSessionMsg struct{ ID string }
type CloseAgentViewMsg struct{}
type OpenAgentViewMsg struct{}
type agentTickMsg time.Time

func NewAgentView() *AgentView {
	ti := textinput.New()
	ti.Placeholder = "reply to selected agent…"
	ti.CharLimit = 4000
	ti.Prompt = "↵ "
	return &AgentView{reply: ti}
}

func (a *AgentView) Open()            { a.visible = true; a.reply.Focus(); a.Refresh() }
func (a *AgentView) Close()           { a.visible = false; a.reply.Blur() }
func (a *AgentView) IsOpen() bool     { return a.visible }
func (a *AgentView) Rows() []AgentRow { return a.rows }

func (a *AgentView) Refresh() {
	statuses, err := bgreg.List()
	if err != nil {
		a.err = err.Error()
		return
	}
	a.err = ""
	a.rows = a.rows[:0]
	for _, s := range statuses {
		a.rows = append(a.rows, AgentRow{
			SessionID:    s.SessionID,
			State:        s.State,
			Task:         s.Task,
			LastResponse: s.LastResponse,
			Updated:      s.UpdatedAt,
		})
	}
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

func (a *AgentView) Init() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return agentTickMsg(t) })
}

func (a *AgentView) Update(msg tea.Msg) (*AgentView, tea.Cmd) {
	if !a.visible {
		return a, nil
	}
	switch m := msg.(type) {
	case agentTickMsg:
		a.Refresh()
		return a, tea.Tick(time.Second, func(t time.Time) tea.Msg { return agentTickMsg(t) })
	case tea.KeyMsg:
		switch m.String() {
		case "esc", "q":
			return a, func() tea.Msg { return CloseAgentViewMsg{} }
		case "up", "k":
			if a.selected > 0 {
				a.selected--
			}
			return a, nil
		case "down", "j":
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

func (a *AgentView) Render(width, height int) string {
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
				marker = avSelStyle.Render("▸")
			}
			lines = append(lines, fmt.Sprintf("%s %s  %-10s  %-8s  ⬢ %s",
				marker,
				fmt.Sprintf("%-40s", agentTruncate(r.Task, 40)),
				stateLabel(r.State),
				humanAgeShort(r.Updated),
				shortName(r.SessionID),
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
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")).Render("error: "+a.err))
	}
	_ = height
	return strings.Join(lines, "\n")
}

func stateLabel(s bgreg.State) string {
	switch s {
	case bgreg.StateActive:
		return lipgloss.NewStyle().Foreground(accentBusy).Render(string(s))
	case bgreg.StateAwaiting:
		return lipgloss.NewStyle().Foreground(accentHoney).Render(string(s))
	case bgreg.StateFailed:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5f5f")).Render(string(s))
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

// agentTruncate clips s to n runes-ish with an ellipsis. Local helper to
// avoid colliding with package-level truncate/wrap utilities in other files.
func agentTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func agentWrap(s string, n int) string {
	if n <= 10 || len(s) <= n {
		return s
	}
	var b strings.Builder
	for len(s) > n {
		cut := n
		for cut > 0 && s[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = n
		}
		b.WriteString(s[:cut])
		b.WriteByte('\n')
		s = strings.TrimLeft(s[cut:], " ")
	}
	b.WriteString(s)
	return b.String()
}
