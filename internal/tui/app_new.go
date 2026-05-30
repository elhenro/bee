package tui

import (
	"os"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"

	"github.com/elhenro/bee/internal/caveman"
	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
)

// NewModel constructs an idle TUI model. eng may be nil for unit tests.
// A built-in slash command registry is created and seeded — callers that
// want a custom set should call WithCommands on the returned Model.
func NewModel(eng *loop.Engine, cwd, modelName, scope string, lvl caveman.Level) Model {
	ti := textarea.New()
	ti.Placeholder = ""
	ti.Prompt = "› "
	ti.ShowLineNumbers = false
	ti.CharLimit = 16384
	ti.SetHeight(1)
	ti.SetWidth(40)
	// kill the default rounded border + cell padding so the textarea sits
	// flush like the old textinput.
	ti.FocusedStyle.Base = lipgloss.NewStyle()
	ti.BlurredStyle.Base = lipgloss.NewStyle()
	ti.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ti.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ti.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(colorHoney)
	ti.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(colorHoney)
	ti.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(colorDim)
	ti.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(colorDim)
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

	thinking := string(llm.ThinkingOff)
	if eng != nil && eng.Cfg.Thinking != "" {
		thinking = string(llm.ParseThinking(eng.Cfg.Thinking))
	}

	mode := string(loop.ModeEdit)
	if eng != nil && eng.Cfg.Mode != "" {
		mode = string(loop.ParseMode(eng.Cfg.Mode))
		// resolve auto: local providers skip the classifier, land in edit.
		if mode == "auto" && (eng.Cfg.Profile == "tiny" || config.IsLocalProvider(eng.Cfg.DefaultProvider)) {
			mode = "edit"
		}
	}

	var pk *Picker
	if eng != nil {
		pk = NewPicker(eng.Cfg)
	}

	return Model{
		styles:   styles,
		keys:     keys,
		state:    StateIdle,
		input:    ti,
		cwd:      cwd,
		model:    modelName,
		scope:    scope,
		caveLvl:  lvl,
		thinking: thinking,
		mode:     mode,
		eng:      eng,
		approval:     NewApprovalModel(styles, keys),
		askModel:     NewAskModel(styles),
		updatePrompt: NewUpdatePrompt(styles),
		stream:   NewStreamRenderer(styles, 80),
		cmds:     reg,
		palette:  NewPalette(reg, nil),
		tree:     NewSessionTree(),
		resume:   NewResumePicker(),
		history:  NewHistoryPicker(),
		picker:   pk,
		effortPane:   NewEffortPane(),
		settingsPane: NewSettingsPane(),
		toolsPane:    NewToolsPane(),
		hive:         NewHive(),
		agentView:    NewAgentView(),
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
	}
}
