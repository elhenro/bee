package tui

import (
	"os"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/checkpoint"
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
)

// NewModel constructs an idle TUI model. eng may be nil for unit tests.
// A built-in slash command registry is created and seeded — callers that
// want a custom set should call WithCommands on the returned Model.
func NewModel(eng *loop.Engine, cwd, modelName, scope string, lvl caveman.Level) Model {
	ti := textarea.New()
	ti.Placeholder = "Ask anything…"
	ti.Prompt = promptFirst
	ti.ShowLineNumbers = false
	ti.CharLimit = 16384
	ti.SetHeight(1)
	ti.SetWidth(40)
	for _, st := range []*textarea.Style{&ti.FocusedStyle, &ti.BlurredStyle} {
		// kill the default rounded border + cell padding so the textarea sits
		// flush like the old textinput.
		st.Base = lipgloss.NewStyle()
		// cursor line shares the text color: the textarea paints the active row
		// with CursorLine instead of Text, so a bare style would dim the row you
		// type on.
		st.CursorLine = lipgloss.NewStyle().Foreground(fgButter)
		st.Prompt = lipgloss.NewStyle().Foreground(colorHoney)
		st.Placeholder = lipgloss.NewStyle().Foreground(fgOyster)
		st.Text = lipgloss.NewStyle().Foreground(fgButter)
	}
	ti.Focus()
	ti.Cursor.SetMode(cursor.CursorStatic)
	// enter is reserved for submit (handleKey catches it before Update).
	// Newline binds to shift+enter (modern terminals: Ghostty, Kitty,
	// Wezterm, iTerm w/ CSI u) and ctrl+j (universal fallback — terminals
	// that don't distinguish shift+enter still send ctrl+j on C-j).
	ti.KeyMap.InsertNewline = key.NewBinding(
		key.WithKeys("shift+enter", "ctrl+j"),
		key.WithHelp("shift+enter", "newline"),
	)

	styles := DefaultStyles()
	keys := DefaultKeyMap()

	reg := commands.NewRegistry()
	commands.RegisterBuiltins(reg)

	role := string(loop.RoleWorker)
	if eng != nil && eng.Cfg.Role != "" {
		role = string(loop.ParseRole(eng.Cfg.Role))
	}
	// thinking mirrors the role's baked budget unless the user pinned an
	// explicit override in config.
	thinking := roleThinking(role)
	if eng != nil && eng.Cfg.Thinking != "" {
		thinking = string(llm.ParseThinking(eng.Cfg.Thinking))
	}

	var pk *Picker
	if eng != nil {
		pk = NewPicker(eng.Cfg)
	}

	// best-effort: a nil store disables checkpoints rather than failing startup.
	var ckpt *checkpoint.Store
	if cwd != "" {
		ckpt, _ = checkpoint.Open(cwd)
	}

	return Model{
		styles:          &styles,
		keys:            keys,
		state:           StateIdle,
		input:           ti,
		cwd:             cwd,
		model:           modelName,
		scope:           scope,
		caveLvl:         lvl,
		thinking:        thinking,
		role:            role,
		yolo:            eng != nil && eng.Cfg.Yolo,
		eng:             eng,
		approval:        NewApprovalModel(styles, keys),
		askModel:        NewAskModel(styles),
		escalate:        NewEscalateModel(styles),
		planmode:        NewPlanModeModel(styles),
		updatePrompt:    NewUpdatePrompt(styles),
		stream:          NewStreamRenderer(styles, 80),
		scroll:          &scrollback{},
		cmds:            reg,
		palette:         NewPalette(reg, nil),
		tree:            NewSessionTree(),
		resume:          NewResumePicker(),
		rewind:          NewRewindPicker(),
		checkpoints:     ckpt,
		history:         NewHistoryPicker(),
		picker:          pk,
		rolePane:        NewRolePane(),
		effortPane:      NewEffortPane(),
		settingsPane:    NewSettingsPane(),
		toolsPane:       NewToolsPane(),
		agentView:       NewAgentView(),
		showThoughts:    true,
		highlight:       true,
		shellBangSilent: true,
		showBee:         true,
		showContextPct:  true,
		showModel:       true,
		showCwd:         true,
		showEffort:      true,
		showTurnTimer:   true,
		// progressive flush on by default: pushes settled head lines of a
		// long streaming response into native terminal scrollback so the user
		// can read from the start while the tail keeps growing. Opt out with
		// BEE_STREAM_PROGRESSIVE=0 to fall back to pure tail-clipping.
		progressiveStream: os.Getenv("BEE_STREAM_PROGRESSIVE") != "0",
		// outerPadLR left at 0; the cfg-driven seed in app_run.go seeds it
		// from eng.Cfg.OuterPadLR when an engine is present, so the default
		// 0 ships off in both the test (eng==nil) and the live (eng!=nil) path.
	}
}
