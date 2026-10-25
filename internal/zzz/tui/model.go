package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/zzz"
	"github.com/elhenro/bee/internal/zzz/uikit"
)

// Model is the bubbletea state for `bee zzz`. It implements zzz.UI by
// pushing every status mutation through a thread-safe channel that Update
// drains via tea.Msg dispatching — Drive runs in a goroutine and never
// touches Model fields directly.
type Model struct {
	run    *zzz.Run
	cfg    zzz.Config
	width  int
	height int

	input  textarea.Model
	tick   int
	now    time.Time
	start  time.Time
	iter   int
	maxIt  int
	phase  string
	tokens zzz.TokenStat
	rows   []iterRow
	log    []logMsg

	box     *uikit.Mailbox // ui events → Update
	steer   chan zzz.Steer
	done    bool
	final   *zzz.Run
	finalEr error

	// stopArmed flips on the first ctrl+c (graceful stop). A second ctrl+c
	// force-quits the program so a wedged iteration — one the engine can't
	// drain steering into — can still be killed via ctx-cancel on Run() exit.
	stopArmed bool
}

// New builds the bubbletea model. width=0 defers sizing until the first
// WindowSizeMsg lands.
func New(run *zzz.Run, cfg zzz.Config) *Model {
	ti := textarea.New()
	ti.Placeholder = "type a nudge or /stop /abort /note <text>"
	ti.Prompt = styHoney.Render("› ") + " "
	ti.ShowLineNumbers = false
	ti.CharLimit = 4096
	ti.SetHeight(2)
	ti.SetWidth(80)
	ti.FocusedStyle.Base = lipgloss.NewStyle()
	ti.BlurredStyle.Base = lipgloss.NewStyle()
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(fgOyster)
	ti.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(fgOyster)
	ti.FocusedStyle.Prompt = styHoney
	ti.BlurredStyle.Prompt = styHoney
	ti.Focus()
	ti.Cursor.SetMode(cursor.CursorStatic)
	ti.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)

	return &Model{
		run:   run,
		cfg:   cfg,
		input: ti,
		start: time.Now(),
		now:   time.Now(),
		box:   uikit.NewMailbox(64),
		steer: make(chan zzz.Steer, 16),
	}
}

// Steer satisfies zzz.Steerable.
func (m *Model) Steer() <-chan zzz.Steer { return m.steer }

// --- zzz.UI implementation (called from the Drive goroutine) ---

func (m *Model) SetIter(n, max int)        { m.box.Send(iterMsg{n: n, max: max}) }
func (m *Model) SetPhase(p string)         { m.box.Send(phaseMsg(p)) }
func (m *Model) SetTokens(t zzz.TokenStat) { m.box.Send(tokensMsg(t)) }
func (m *Model) IncCommits()               {} // the panels show no commit count
func (m *Model) Println(msg string)        { m.box.Send(logMsg{level: uikit.LevelFor(msg), text: msg}) }
func (m *Model) RenderSummary(r *zzz.Run)  {} // finalPanel() shows the summary

// Done is called by the launcher after Drive returns so the TUI can show
// the final panel and let the user dismiss with q.
func (m *Model) Done(r *zzz.Run, err error) {
	m.box.Send(doneMsg{run: r, err: err})
}
