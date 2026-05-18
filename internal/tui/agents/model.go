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
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/tui"
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

	cmds    *commands.Registry
	palette tui.PaletteModel

	prefs        Prefs
	settingsPane *settingsPane
	picker       *tui.Picker
	pickerOpen   bool
	cfg          config.Config

	exitReq  bool
	attachID string
}

type tickMsg time.Time

func newModel(repoRoot string) model {
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
	reg := newAgentsCmdRegistry()
	prefs := LoadPrefs()
	cfg, err := config.Load()
	if err != nil && len(cfg.Providers) == 0 {
		cfg = config.Defaults()
	}
	if prefs.DefaultModel == "" {
		prefs.DefaultModel = cfg.DefaultModel
	}
	if prefs.DefaultProvider == "" {
		prefs.DefaultProvider = cfg.DefaultProvider
	}
	return model{
		repoRoot:        repoRoot,
		input:           ti,
		cmds:            reg,
		palette:         tui.NewPalette(reg, nil),
		prefs:           prefs,
		pendingModel:    prefs.DefaultModel,
		pendingProvider: prefs.DefaultProvider,
		settingsPane:    newSettingsPane(),
		picker:          tui.NewPicker(cfg),
		cfg:             cfg,
	}
}

// newAgentsCmdRegistry builds palette metadata for the overview view. Run
// funcs are stubs — dispatch happens in runSlash (no engine/Side here).
func newAgentsCmdRegistry() *commands.Registry {
	r := commands.NewRegistry()
	add := func(name, desc string) {
		r.Register(commands.Command{
			Name:           name,
			Description:    desc,
			AllowDuringRun: true,
			Run: func(context.Context, []string, commands.Side) (string, error) {
				return "", nil
			},
		})
	}
	add("model", "set next-spawn model (e.g. /model claude-sonnet-4-6)")
	add("provider", "set next-spawn provider (e.g. /provider anthropic)")
	add("settings", "toggle overview view options (peek, badges, chip, …)")
	add("help", "show keys & slash commands")
	add("quit", "exit overview")
	add("exit", "exit overview")
	return r
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
		m.palette.SetWidth(msg.Width)
		if m.picker != nil {
			m.picker.SetSize(msg.Width-4, msg.Height-4)
		}
		return m, nil

	case tickMsg:
		m = m.refresh()
		if m.picker != nil && m.picker.Active() {
			newP, cmd := m.picker.Update(msg)
			m.picker = newP
			if m.exitReq {
				return m, tea.Quit
			}
			return m, tea.Batch(tickCmd(), cmd)
		}
		if m.exitReq {
			return m, tea.Quit
		}
		return m, tickCmd()

	case refreshMsg:
		m = m.refresh()
		return m, nil

	case tui.PaletteSelectMsg:
		m.input.SetValue("/" + msg.Name)
		// submit immediately: dispatch through runSlash.
		text := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		return m.runSlash(text)

	case tui.PaletteDismissedMsg:
		// clear staged "/foo" — user cancelled.
		if strings.HasPrefix(m.input.Value(), "/") {
			m.input.Reset()
		}
		return m, nil

	case tui.PickedMsg:
		m.applyPick(msg.Provider, msg.Model)
		return m, nil

	case tui.PickerDismissedMsg:
		m.pickerOpen = false
		return m, nil

	case tui.PickerLoginRequestedMsg:
		m.flash("run /login " + msg.Provider + " in main bee")
		return m, nil

	case agentsSettingsToggleMsg:
		applyPrefToggle(&m.prefs, msg.key, msg.value)
		if err := persistToggle(msg.key, msg.value); err != nil {
			m.flash("persist failed: " + err.Error())
		}
		return m, nil

	case tea.KeyMsg:
		if m.picker != nil && m.picker.Active() {
			newP, cmd := m.picker.Update(msg)
			m.picker = newP
			return m, cmd
		}
		// settings pane claims keys while open; everything else flows through.
		if m.settingsPane.isOpen() {
			cmd := m.settingsPane.update(msg)
			return m, cmd
		}
		nm, cmd := m.handleKey(msg)
		// mirror input → palette filter; close palette if user backspaced past
		// "/" or started typing args (space after the command name).
		if mm, ok := nm.(model); ok && mm.palette.Active {
			val := mm.input.Value()
			switch {
			case !strings.HasPrefix(val, "/"):
				mm.palette.Active = false
			case strings.Contains(val, " "):
				// args mode: hand the line off to runSlash on enter
				mm.palette.Active = false
			default:
				mm.palette.SetFilter(val[1:])
			}
			return mm, cmd
		}
		return nm, cmd
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
	if m.settingsPane.isOpen() {
		return m.settingsPane.view(m.width, m.height)
	}
	var b strings.Builder
	b.WriteString(renderHeader(m.all, m.pendingModel, m.pendingProvider, m.prefs))
	b.WriteString("\n\n")

	if len(m.flat) == 0 {
		b.WriteString(dimStyle.Render("  no agents yet — type a task below and press enter to spawn one."))
		b.WriteString("\n\n")
	} else {
		idx := 0
		for _, sec := range m.sections {
			if sec.kind == secMerged && !m.prefs.ShowMerged {
				// hidden section still consumes the flat-index range so the
				// arrow-key cursor stays in sync with what's drawn.
				idx += len(sec.rows)
				continue
			}
			style := sectionStyle
			if sec.kind == secErrors {
				style = errSectionStyle
			}
			label := style.Render(fmt.Sprintf(" ▸ %s (%d)", sec.kind.title(), len(sec.rows)))
			label += dimStyle.Render(sectionHint(sec))
			b.WriteString(label)
			b.WriteString("\n")
			for _, r := range sec.rows {
				selected := idx == m.sel
				b.WriteString(renderRow(r, selected, m.width, m.prefs))
				b.WriteString("\n")
				idx++
			}
			b.WriteString("\n")
		}
	}

	if m.palette.Active {
		b.WriteString(m.palette.View())
		b.WriteString("\n")
	}
	if m.picker != nil && m.picker.Active() {
		b.WriteString(m.picker.View())
		b.WriteString("\n")
	}
	b.WriteString(m.input.View())
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
