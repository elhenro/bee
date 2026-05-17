package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsRow describes one toggleable row in the settings pane.
type settingsRow struct {
	key   string // toml key persisted to ~/.bee/config.toml
	label string // human-readable label
	desc  string // one-line description shown next to the toggle
}

var settingsRows = []settingsRow{
	{key: "verbose", label: "verbose tool output", desc: "show full tool result instead of one-line preview"},
	{key: "show_thoughts", label: "show agent thoughts", desc: "render chain-of-thought reasoning blocks"},
	{key: "show_nudges", label: "show nudge messages", desc: "show the agent's self-nudge recovery messages in the transcript"},
	{key: "compact", label: "compact layout", desc: "drop gutter + inter-turn blank + user tint + OSC 133"},
	{key: "show_context_bar", label: "show context bar", desc: "thin context-fill strip at the bottom edge"},
	{key: "highlight", label: "syntax highlight", desc: "color code in diffs, file content, and bash commands"},
	{key: "shell_bang_silent", label: "!shell stays local", desc: "!cmd runs without forwarding output to the LLM (!! inverts)"},
}

// SettingsPane is a modal toggling persistent TUI settings. Arrow keys move
// cursor; enter/space flips the focused row. Each flip applies live and writes
// to ~/.bee/config.toml so the next launch picks the same values up.
type SettingsPane struct {
	open       bool
	cursor     int
	verbose    bool
	thought    bool
	nudge      bool
	compact    bool
	ctxBar     bool
	highlight  bool
	bangSilent bool
}

// NewSettingsPane returns a closed settings pane.
func NewSettingsPane() *SettingsPane {
	return &SettingsPane{thought: true, highlight: true, bangSilent: true}
}

// Open reports visibility.
func (p *SettingsPane) Open() bool { return p != nil && p.open }

// Show opens the pane seeded with the live values.
func (p *SettingsPane) Show(verbose, showThoughts, showNudges, compact, showContextBar, highlight, shellBangSilent bool) {
	if p == nil {
		return
	}
	p.open = true
	p.cursor = 0
	p.verbose = verbose
	p.thought = showThoughts
	p.nudge = showNudges
	p.compact = compact
	p.ctxBar = showContextBar
	p.highlight = highlight
	p.bangSilent = shellBangSilent
}

// settingsToggleMsg is published when a row is toggled. Carries the new value
// for the affected key so Model.Update can apply + persist atomically.
type settingsToggleMsg struct {
	key   string
	value bool
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
	case "esc", "q":
		p.open = false
	case "down", "j":
		if p.cursor < len(settingsRows)-1 {
			p.cursor++
		}
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}
	case "enter", " ", "tab":
		row := settingsRows[p.cursor]
		var newVal bool
		switch row.key {
		case "verbose":
			p.verbose = !p.verbose
			newVal = p.verbose
		case "show_thoughts":
			p.thought = !p.thought
			newVal = p.thought
		case "show_nudges":
			p.nudge = !p.nudge
			newVal = p.nudge
		case "compact":
			p.compact = !p.compact
			newVal = p.compact
		case "show_context_bar":
			p.ctxBar = !p.ctxBar
			newVal = p.ctxBar
		case "highlight":
			p.highlight = !p.highlight
			newVal = p.highlight
		case "shell_bang_silent":
			p.bangSilent = !p.bangSilent
			newVal = p.bangSilent
		}
		return p, func() tea.Msg { return settingsToggleMsg{key: row.key, value: newVal} }
	}
	return p, nil
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

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")

	for i, r := range settingsRows {
		marker := "  "
		nameStyle := lipgloss.NewStyle().Foreground(fgOyster)
		if i == p.cursor {
			marker = lipgloss.NewStyle().Foreground(accentHoney).Render("▸ ")
			nameStyle = nameStyle.Foreground(accentHoney).Bold(true)
		}
		var state bool
		switch r.key {
		case "verbose":
			state = p.verbose
		case "show_thoughts":
			state = p.thought
		case "show_nudges":
			state = p.nudge
		case "compact":
			state = p.compact
		case "show_context_bar":
			state = p.ctxBar
		case "highlight":
			state = p.highlight
		case "shell_bang_silent":
			state = p.bangSilent
		}
		toggle := "[ ]"
		if state {
			toggle = lipgloss.NewStyle().Foreground(accentHoney).Render("[x]")
		}
		b.WriteString(marker)
		b.WriteString(toggle)
		b.WriteString("  ")
		b.WriteString(nameStyle.Render(padRightVisible(r.label, 22)))
		b.WriteString("  ")
		b.WriteString(StyleLabel.Render(r.desc))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(StyleLabel.Render("↑/↓ pick · enter/space toggle · esc close · saved to ~/.bee/config.toml"))
	return boxModal(b.String(), width, height)
}
