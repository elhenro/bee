package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// settingsRow describes one toggleable row in the settings pane.
type settingsRow struct {
	key   string // toml key persisted to ~/.bee/config.toml
	label string // human-readable label
	desc  string // one-line description shown next to the toggle
}

// settingsRows is the alphabetised pool. Sorted at package init so new rows
// can be appended in any order without resorting the source file.
var settingsRows = []settingsRow{
	{key: "verbose", label: "verbose tool output", desc: "show full tool result instead of one-line preview"},
	{key: "show_thoughts", label: "show agent thoughts", desc: "render chain-of-thought reasoning blocks"},
	{key: "show_nudges", label: "show nudge messages", desc: "show the agent's self-nudge recovery messages in the transcript"},
	{key: "compact", label: "compact layout", desc: "drop gutter + inter-turn blank + user tint + OSC 133"},
	{key: "show_context_bar", label: "show context bar", desc: "thin context-fill strip at the bottom edge"},
	{key: "highlight", label: "syntax highlight", desc: "color code in diffs, file content, and bash commands"},
	{key: "shell_bang_silent", label: "!shell stays local", desc: "!cmd runs without forwarding output to the LLM (!! inverts)"},
	{key: "show_banner", label: "show intro animation", desc: "braille startup animation (applies on next launch)"},
	{key: "show_loader", label: "show generating animation", desc: "braille loader + caret while the model is generating"},
	{key: "show_bee", label: "top-bar bee glyph", desc: "show the 🐝 emoji on the top status line"},
	{key: "show_context_pct", label: "top-bar context %", desc: "show the percent label next to the bee glyph"},
	{key: "show_model", label: "top-bar model name", desc: "show the active provider/model label"},
	{key: "show_cwd", label: "top-bar cwd", desc: "show the current working directory"},
	{key: "show_effort", label: "top-bar effort", desc: "show the thinking-effort badge (t:max)"},
	{key: "show_turn_timer", label: "top-bar turn timer", desc: "show ⏱ live elapsed while working + final time after"},
	{key: "show_git_branch", label: "top-bar git branch", desc: "show ⎇ current git branch (when cwd is in a repo)"},
	{key: "show_total_tokens", label: "top-bar total tokens", desc: "show Σ session tokens (input+output) next to cost"},
}

func init() {
	sort.Slice(settingsRows, func(i, j int) bool {
		return settingsRows[i].label < settingsRows[j].label
	})
}

// SettingsPane is a modal toggling persistent TUI settings. Arrow keys move
// cursor, enter/space flips the focused row, anything else types into the
// fuzzy filter. Each flip applies live and writes to ~/.bee/config.toml so
// the next launch picks the same values up.
type SettingsPane struct {
	open       bool
	cursor     int
	filter     textinput.Model
	matches    []fuzzy.Match
	verbose    bool
	thought    bool
	nudge      bool
	compact    bool
	ctxBar     bool
	highlight  bool
	bangSilent bool
	bee        bool
	ctxPct     bool
	modelName  bool
	cwd        bool
	effort     bool
	turnTimer  bool
	gitBranch  bool
	totTokens  bool
	banner     bool
	loader     bool
}

// NewSettingsPane returns a closed settings pane.
func NewSettingsPane() *SettingsPane {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Prompt = "› "
	ti.CharLimit = 64
	p := &SettingsPane{
		filter:     ti,
		thought:    true,
		highlight:  true,
		bangSilent: true,
		bee:        true,
		ctxPct:     true,
		modelName:  true,
		cwd:        true,
		effort:     true,
		turnTimer:  true,
		banner:     true,
		loader:     true,
	}
	p.recomputeMatches()
	return p
}

// Open reports visibility.
func (p *SettingsPane) Open() bool { return p != nil && p.open }

// SettingsSnapshot is the live values handed to Show. Grouped into a struct
// because the top-bar toggles pushed the positional-arg list past readable.
type SettingsSnapshot struct {
	Verbose         bool
	ShowThoughts    bool
	ShowNudges      bool
	Compact         bool
	ShowContextBar  bool
	Highlight       bool
	ShellBangSilent bool
	ShowBee         bool
	ShowContextPct  bool
	ShowModel       bool
	ShowCwd         bool
	ShowEffort      bool
	ShowTurnTimer   bool
	ShowGitBranch   bool
	ShowTotalTokens bool
	ShowBanner      bool
	ShowLoader      bool
}

// Show opens the pane seeded with the live values.
func (p *SettingsPane) Show(s SettingsSnapshot) {
	if p == nil {
		return
	}
	p.open = true
	p.cursor = 0
	p.filter.SetValue("")
	p.filter.Focus()
	p.verbose = s.Verbose
	p.thought = s.ShowThoughts
	p.nudge = s.ShowNudges
	p.compact = s.Compact
	p.ctxBar = s.ShowContextBar
	p.highlight = s.Highlight
	p.bangSilent = s.ShellBangSilent
	p.bee = s.ShowBee
	p.ctxPct = s.ShowContextPct
	p.modelName = s.ShowModel
	p.cwd = s.ShowCwd
	p.effort = s.ShowEffort
	p.turnTimer = s.ShowTurnTimer
	p.gitBranch = s.ShowGitBranch
	p.totTokens = s.ShowTotalTokens
	p.banner = s.ShowBanner
	p.loader = s.ShowLoader
	p.recomputeMatches()
}

// settingsToggleMsg is published when a row is toggled. Carries the new value
// for the affected key so Model.Update can apply + persist atomically.
type settingsToggleMsg struct {
	key   string
	value bool
}

// recomputeMatches fuzzy-filters settingsRows against the filter input. Empty
// input keeps the full alphabetised list (one code path for the renderer).
func (p *SettingsPane) recomputeMatches() {
	needle := strings.TrimSpace(p.filter.Value())
	if needle == "" {
		p.matches = make([]fuzzy.Match, len(settingsRows))
		for i, r := range settingsRows {
			p.matches[i] = fuzzy.Match{Index: i, Str: r.label}
		}
	} else {
		// match against "label  description" so descriptions can rank too;
		// the renderer masks highlight indices to the label range.
		hay := make([]string, len(settingsRows))
		for i, r := range settingsRows {
			hay[i] = r.label + "  " + r.desc
		}
		p.matches = fuzzy.Find(needle, hay)
	}
	if p.cursor >= len(p.matches) {
		p.cursor = 0
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

// Update handles key events.
func (p *SettingsPane) Update(msg tea.Msg) (*SettingsPane, tea.Cmd) {
	if p == nil || !p.open {
		return p, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	switch km.String() {
	case "esc", "ctrl+c":
		p.open = false
		return p, nil
	case "down", "ctrl+n":
		if p.cursor < len(p.matches)-1 {
			p.cursor++
		}
		return p, nil
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
		return p, nil
	case "enter", "tab":
		return p, p.toggleCurrent()
	}
	prev := p.filter.Value()
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(km)
	if p.filter.Value() != prev {
		p.cursor = 0
		p.recomputeMatches()
	}
	return p, cmd
}

// toggleCurrent flips the row at p.cursor (in the filtered match list) and
// returns the side-effect command publishing the new value.
func (p *SettingsPane) toggleCurrent() tea.Cmd {
	if len(p.matches) == 0 {
		return nil
	}
	idx := p.matches[p.cursor].Index
	if idx < 0 || idx >= len(settingsRows) {
		return nil
	}
	row := settingsRows[idx]
	newVal := !p.rowState(row.key)
	p.setRowState(row.key, newVal)
	return func() tea.Msg { return settingsToggleMsg{key: row.key, value: newVal} }
}

// rowState reads the toggle backing field for a given key.
func (p *SettingsPane) rowState(key string) bool {
	switch key {
	case "verbose":
		return p.verbose
	case "show_thoughts":
		return p.thought
	case "show_nudges":
		return p.nudge
	case "compact":
		return p.compact
	case "show_context_bar":
		return p.ctxBar
	case "highlight":
		return p.highlight
	case "shell_bang_silent":
		return p.bangSilent
	case "show_bee":
		return p.bee
	case "show_context_pct":
		return p.ctxPct
	case "show_model":
		return p.modelName
	case "show_cwd":
		return p.cwd
	case "show_effort":
		return p.effort
	case "show_turn_timer":
		return p.turnTimer
	case "show_git_branch":
		return p.gitBranch
	case "show_total_tokens":
		return p.totTokens
	case "show_banner":
		return p.banner
	case "show_loader":
		return p.loader
	}
	return false
}

// setRowState writes the toggle backing field for a given key.
func (p *SettingsPane) setRowState(key string, v bool) {
	switch key {
	case "verbose":
		p.verbose = v
	case "show_thoughts":
		p.thought = v
	case "show_nudges":
		p.nudge = v
	case "compact":
		p.compact = v
	case "show_context_bar":
		p.ctxBar = v
	case "highlight":
		p.highlight = v
	case "shell_bang_silent":
		p.bangSilent = v
	case "show_bee":
		p.bee = v
	case "show_context_pct":
		p.ctxPct = v
	case "show_model":
		p.modelName = v
	case "show_cwd":
		p.cwd = v
	case "show_effort":
		p.effort = v
	case "show_turn_timer":
		p.turnTimer = v
	case "show_git_branch":
		p.gitBranch = v
	case "show_total_tokens":
		p.totTokens = v
	case "show_banner":
		p.banner = v
	case "show_loader":
		p.loader = v
	}
}

// View renders the modal.
func (p *SettingsPane) View(width, height int) string {
	if p == nil || !p.open {
		return ""
	}
	title := lipgloss.NewStyle().
		Foreground(accentHoney).
		Bold(true).
		Render("⬢ Settings")

	hl := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(p.filter.View())
	b.WriteString("\n\n")

	if len(p.matches) == 0 {
		b.WriteString(StyleLabel.Render("  no matches"))
		b.WriteString("\n")
	}

	for i, m := range p.matches {
		r := settingsRows[m.Index]
		marker := "  "
		nameStyle := lipgloss.NewStyle().Foreground(fgOyster)
		if i == p.cursor {
			marker = lipgloss.NewStyle().Foreground(accentHoney).Render("▸ ")
			nameStyle = nameStyle.Foreground(accentHoney).Bold(true)
		}
		toggle := "[ ]"
		if p.rowState(r.key) {
			toggle = lipgloss.NewStyle().Foreground(accentHoney).Render("[x]")
		}
		b.WriteString(marker)
		b.WriteString(toggle)
		b.WriteString("  ")
		b.WriteString(highlightLabel(r.label, m.MatchedIndexes, nameStyle, hl, 22))
		b.WriteString("  ")
		b.WriteString(StyleLabel.Render(r.desc))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("type filter · ↑/↓ pick · enter/tab toggle · esc close · saved to ~/.bee/config.toml"))
	return boxModal(b.String(), width, height)
}

// highlightLabel renders label with matched runes accented, then pads the
// visible width to n columns so toggle/desc columns stay aligned.
func highlightLabel(label string, matched []int, base, hl lipgloss.Style, n int) string {
	hits := map[int]struct{}{}
	for _, idx := range matched {
		if idx >= 0 && idx < len(label) {
			hits[idx] = struct{}{}
		}
	}
	var b strings.Builder
	for i := 0; i < len(label); i++ {
		ch := string(label[i])
		if _, ok := hits[i]; ok {
			b.WriteString(hl.Render(ch))
		} else {
			b.WriteString(base.Render(ch))
		}
	}
	pad := n - len(label)
	if pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	return b.String()
}
