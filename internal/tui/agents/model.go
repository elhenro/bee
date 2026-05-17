package agents

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/agents"
	"github.com/elhenro/bee/internal/bgreg"
)

// Result is what Run returns after the overview exits. AttachID set means
// the caller should open `bee back <id>`; empty means clean quit.
type Result struct {
	AttachID string
}

// model is the bubbletea state for the overview.
type model struct {
	width, height int

	repoRoot string

	input textarea.Model

	all      []bgreg.Status
	flat     []row
	sections []section
	sel      int

	pendingModel    string
	pendingProvider string

	notice    string // transient error / info line
	noticeTTL time.Time

	retryCh chan<- string // sends session ids to the merger goroutine

	exitReq  bool
	attachID string
}

type tickMsg time.Time

func newModel(repoRoot string, retryCh chan<- string) model {
	ti := textarea.New()
	ti.Placeholder = "type a task and press enter to spawn an agent…"
	ti.Prompt = "› "
	ti.ShowLineNumbers = false
	ti.CharLimit = 8192
	ti.SetHeight(1)
	ti.SetWidth(60)
	ti.FocusedStyle.Base = lipgloss.NewStyle()
	ti.BlurredStyle.Base = lipgloss.NewStyle()
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ti.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(honey)
	ti.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(honey)
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(dim)
	ti.Focus()
	return model{
		repoRoot: repoRoot,
		input:    ti,
		retryCh:  retryCh,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		refreshCmd(),
		tickCmd(),
	)
}

type refreshMsg struct{}

func refreshCmd() tea.Cmd {
	return func() tea.Msg { return refreshMsg{} }
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) refresh() model {
	all, _ := bgreg.List()
	m.all = all
	m.sections = buildSections(all)
	m.flat = flatten(m.sections)
	if m.sel >= len(m.flat) {
		m.sel = 0
	}
	if m.sel < 0 {
		m.sel = 0
	}
	if m.notice != "" && time.Now().After(m.noticeTTL) {
		m.notice = ""
	}
	return m
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(max(20, msg.Width-4))
		return m, nil

	case tickMsg:
		m = m.refresh()
		if m.exitReq {
			return m, tea.Quit
		}
		return m, tickCmd()

	case refreshMsg:
		m = m.refresh()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// flash sets a brief notice line at the bottom of the view.
func (m *model) flash(s string) {
	m.notice = s
	m.noticeTTL = time.Now().Add(3 * time.Second)
}

// selected returns the currently focused row, ok=false if empty list.
func (m model) selected() (row, bool) {
	if m.sel < 0 || m.sel >= len(m.flat) {
		return row{}, false
	}
	return m.flat[m.sel], true
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(renderHeader(m.all, m.pendingModel, m.pendingProvider))
	b.WriteString("\n\n")

	if len(m.flat) == 0 {
		b.WriteString(dimStyle.Render("  no agents yet — type a task below and press enter to spawn one."))
		b.WriteString("\n\n")
	} else {
		idx := 0
		for _, sec := range m.sections {
			label := sectionStyle.Render(fmt.Sprintf(" ▸ %s (%d)", sec.kind.title(), len(sec.rows)))
			if sec.kind == secDoneUnmerged {
				label += dimStyle.Render("   [press m to re-merge]")
			}
			if sec.kind == secNeedsInput {
				label += dimStyle.Render("   [press m to retry merge]")
			}
			b.WriteString(label)
			b.WriteString("\n")
			for _, r := range sec.rows {
				selected := idx == m.sel
				b.WriteString(renderRow(r, selected, m.width))
				b.WriteString("\n")
				idx++
			}
			b.WriteString("\n")
		}
	}

	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(hintStyle.Render("enter=spawn · l/→=open · h/←=back · m=merge · /model · /provider · ctrl+c=quit"))
	if m.notice != "" {
		b.WriteString("\n")
		b.WriteString(badStyle.Render("• " + m.notice))
	}
	return b.String()
}

// spawnAgent calls agents.Spawn for the current input + pending model/provider.
func (m *model) spawnAgent(prompt string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = ctx
	res, err := agents.Spawn(agents.SpawnOpts{
		Prompt:   prompt,
		Model:    m.pendingModel,
		Provider: m.pendingProvider,
		RepoRoot: m.repoRoot,
	})
	if err != nil {
		m.flash("spawn failed: " + err.Error())
		return
	}
	m.flash("spawned " + res.SessionID[:8] + " on " + res.Branch)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
