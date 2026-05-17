package tui

import (
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/zzz"
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

	// channels
	msgs    chan tea.Msg // ui events → Update
	steer   chan zzz.Steer
	done    bool
	final   *zzz.Run
	finalEr error

	mu sync.Mutex // guards rows, log, iter, phase mutated by Send
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
		msgs:  make(chan tea.Msg, 64),
		steer: make(chan zzz.Steer, 16),
	}
}

// Steer satisfies zzz.Steerable.
func (m *Model) Steer() <-chan zzz.Steer { return m.steer }

// --- zzz.UI implementation (called from the Drive goroutine) ---

func (m *Model) SetIter(n, max int)         { m.send(iterMsg{n: n, max: max}) }
func (m *Model) SetPhase(p string)          { m.send(phaseMsg(p)) }
func (m *Model) SetTokens(t zzz.TokenStat)  { m.send(tokensMsg(t)) }
func (m *Model) IncCommits()                { m.send(commitsMsg(1)) }
func (m *Model) Println(msg string)         { m.send(logMsg{level: levelFor(msg), text: msg}) }
func (m *Model) RenderSummary(r *zzz.Run)   { /* TUI shows summary in finalPanel(); printing is a no-op */ }

// Done is called by the launcher after Drive returns so the TUI can show
// the final panel and let the user dismiss with q.
func (m *Model) Done(r *zzz.Run, err error) {
	m.send(doneMsg{run: r, err: err})
}

func (m *Model) send(msg tea.Msg) {
	select {
	case m.msgs <- msg:
	default:
		// drop on saturation; live status is non-critical (events.jsonl is canonical)
	}
}

func levelFor(s string) string {
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "fail"), strings.Contains(low, "error"):
		return "err"
	case strings.Contains(low, "blocked"), strings.Contains(low, "denied"):
		return "warn"
	}
	return "info"
}
