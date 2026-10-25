package tui

import (
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

// settingsRow describes one toggleable row in the settings pane.
type settingsRow struct {
	key   string // toml key persisted to ~/.bee/config.toml
	label string // human-readable label
	desc  string // one-line description shown next to the toggle
	// isInt marks the row as a magnitude (0..N) instead of a bool state.
	// The renderer shows `[N]` instead of `[x]`, and toggleCurrent publishes
	// settingsSetIntMsg instead of settingsToggleMsg on commit.
	isInt bool
}

// settingsRows is the alphabetised pool. Sorted at package init so new rows
// can be appended in any order without resorting the source file.
var settingsRows = []settingsRow{
	{key: "verbose", label: "verbose tool output", desc: "full tool result instead of one-line preview"},
	{key: "show_thoughts", label: "agent thoughts", desc: "chain-of-thought reasoning blocks"},
	{key: "show_nudges", label: "nudge messages", desc: "self-nudge recovery messages in the transcript"},
	{key: "show_recap", label: "post-turn recap", desc: "one-line summary + next step after each turn (costs tokens)"},
	{key: "compact", label: "compact layout", desc: "dense: no gutter, no blank rows between turns, no user panel"},
	{key: "show_context_bar", label: "context gauge", desc: "fill the prompt's bottom rule as the context window fills"},
	{key: "show_call_detail", label: "tool call detail", desc: "whole command or args under each labeled tool call"},
	{key: "highlight", label: "syntax highlight", desc: "color code in diffs, file content, and bash commands"},
	{key: "shell_bang_silent", label: "!shell stays local", desc: "!cmd runs without forwarding output to the LLM (!! inverts)"},
	{key: "show_banner", label: "intro animation", desc: "braille startup animation (applies on next launch)"},
	{key: "show_loader", label: "spinner animation", desc: "animated spinner while generating (off = static mark)"},
	{key: "show_bee", label: "status: bee mark", desc: "⬢ mark beside the context figure in the status line"},
	{key: "show_context_pct", label: "status: context %", desc: "context-window fill label (ctx 12%)"},
	{key: "show_model", label: "status: model name", desc: "active provider/model label"},
	{key: "show_cwd", label: "status: cwd", desc: "current working directory"},
	{key: "show_effort", label: "status: effort", desc: "role + reasoning-budget chips (worker, t:max)"},
	{key: "show_turn_timer", label: "status: turn timer", desc: "how long the last turn took (long turns only)"},
	{key: "show_git_branch", label: "status: git branch", desc: "⎇ current git branch (when cwd is in a repo)"},
	{key: "show_total_tokens", label: "status: total tokens", desc: "session tokens (input+output) next to cost"},
	{key: "show_loader_in", label: "loader: input tokens", desc: "↑ input-token figure on the busy row"},
	{key: "show_loader_out", label: "loader: output tokens", desc: "↓ output-token figure on the busy row"},
	{key: "show_loader_rate", label: "loader: tok/s", desc: "generation throughput on the busy row"},
	// outer_pad_lr is the int row: cycles 0..8, render as `[N]`.
	{key: "outer_pad_lr", label: "outer padding (L/R)", desc: "blank cells left+right around TUI (int 0-8, 0=off; enter cycles)", isInt: true},
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
	open    bool
	cursor  int
	filter  textinput.Model
	matches []fuzzy.Match
	on      map[string]bool // bool rows by toml key
	// outerPad mirrors the int value for the outer_pad_lr row. Renders as
	// `[N]` (0..8) and cycles on enter/tab via settingsSetIntMsg.
	outerPad int
}

// outerPadMax mirrors WithOuterPadLR's upper bound. Used by the cycle path
// so the pane can't ask for a value the setter would clamp anyway.
const outerPadMax = 8

// NewSettingsPane returns a closed settings pane.
func NewSettingsPane() *SettingsPane {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Prompt = " › "
	ti.CharLimit = 64
	p := &SettingsPane{filter: ti, on: map[string]bool{}}
	p.recomputeMatches()
	return p
}

// Open reports visibility.
func (p *SettingsPane) Open() bool { return p != nil && p.open }

// SettingsSnapshot is the live values handed to Show. Grouped into a struct
// because the status-line toggles pushed the positional-arg list past readable.
type SettingsSnapshot struct {
	Verbose         bool
	ShowThoughts    bool
	ShowNudges      bool
	ShowRecap       bool
	Compact         bool
	ShowContextBar  bool
	Highlight       bool
	ShowCallDetail  bool
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
	ShowLoaderIn    bool
	ShowLoaderOut   bool
	ShowLoaderRate  bool
	// OuterPadLR is the live 0..8 cell count for the left+right padding wrap
	// applied to View() output. Renders as the int row in the settings pane.
	OuterPadLR int
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
	p.on = map[string]bool{
		"verbose":           s.Verbose,
		"show_thoughts":     s.ShowThoughts,
		"show_nudges":       s.ShowNudges,
		"show_recap":        s.ShowRecap,
		"compact":           s.Compact,
		"show_context_bar":  s.ShowContextBar,
		"highlight":         s.Highlight,
		"show_call_detail":  s.ShowCallDetail,
		"shell_bang_silent": s.ShellBangSilent,
		"show_bee":          s.ShowBee,
		"show_context_pct":  s.ShowContextPct,
		"show_model":        s.ShowModel,
		"show_cwd":          s.ShowCwd,
		"show_effort":       s.ShowEffort,
		"show_turn_timer":   s.ShowTurnTimer,
		"show_git_branch":   s.ShowGitBranch,
		"show_total_tokens": s.ShowTotalTokens,
		"show_banner":       s.ShowBanner,
		"show_loader":       s.ShowLoader,
		"show_loader_in":    s.ShowLoaderIn,
		"show_loader_out":   s.ShowLoaderOut,
		"show_loader_rate":  s.ShowLoaderRate,
	}
	p.outerPad = s.OuterPadLR
	p.recomputeMatches()
}

// settingsToggleMsg is published when a row is toggled. Carries the new value
// for the affected key so Model.Update can apply + persist atomically.
type settingsToggleMsg struct {
	key   string
	value bool
}

// settingsSetIntMsg is published when an int row is cycled (the only int row
// today is outer_pad_lr). Same shape as settingsToggleMsg but with an int
// payload, so the dispatcher in app_update.go can route the two message
// kinds to the right Side method.
type settingsSetIntMsg struct {
	key   string
	value int
}

// recomputeMatches fuzzy-filters settingsRows against the filter input. Empty
// input keeps the full alphabetised list (one code path for the renderer).
func (p *SettingsPane) recomputeMatches() {
	// match against "label  description" so descriptions can rank too; the
	// renderer masks highlight indices to the label range.
	p.matches = FuzzyAll(p.filter.Value(), settingsRows, func(i int) string {
		return settingsRows[i].label + "  " + settingsRows[i].desc
	})
	if p.cursor >= len(p.matches) || p.cursor < 0 {
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
// returns the side-effect command publishing the new value. The int row
// (outer_pad_lr) is routed through settingsSetIntMsg and cycled 0..N..0
// instead of bool-flipped.
func (p *SettingsPane) toggleCurrent() tea.Cmd {
	if len(p.matches) == 0 {
		return nil
	}
	idx := p.matches[p.cursor].Index
	if idx < 0 || idx >= len(settingsRows) {
		return nil
	}
	row := settingsRows[idx]
	if row.isInt {
		next := p.outerPad + 1
		if next > outerPadMax {
			next = 0
		}
		p.outerPad = next
		return func() tea.Msg { return settingsSetIntMsg{key: row.key, value: next} }
	}
	newVal := !p.on[row.key]
	p.on[row.key] = newVal
	return func() tea.Msg { return settingsToggleMsg{key: row.key, value: newVal} }
}
