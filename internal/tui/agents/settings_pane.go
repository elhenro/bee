package agents

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	tui "github.com/elhenro/bee/internal/tui"
)

// settingsRow describes one toggleable preference row in the agents pane.
type settingsRow struct {
	key   string
	label string
	desc  string
}

// settingsRowsAgents is the alphabetised pool of overview-only toggles. Keys
// match the TOML names in Prefs so PersistSetting writes the same field the
// next launch will read.
var settingsRowsAgents = []settingsRow{
	{key: "agents_show_peek", label: "show peek line", desc: "render last LLM response under each agent row"},
	{key: "agents_show_badges", label: "show merge badges", desc: "render conflict / merged / merging badges"},
	{key: "agents_show_chip", label: "show model chip", desc: "render [model-name] on each agent row"},
	{key: "agents_show_subheader", label: "show subheader", desc: "show 'next spawn → model: …' line under title"},
	{key: "agents_show_hint", label: "show hint footer", desc: "bottom row with key reminders"},
	{key: "agents_show_merged", label: "show merged section", desc: "list agents whose branches are already merged"},
}

// settingsHayAgents is the fuzzy haystack per row ("label  description"),
// parallel to settingsRowsAgents.
var settingsHayAgents []string

func init() {
	sort.Slice(settingsRowsAgents, func(i, j int) bool {
		return settingsRowsAgents[i].label < settingsRowsAgents[j].label
	})
	for _, r := range settingsRowsAgents {
		settingsHayAgents = append(settingsHayAgents, r.label+"  "+r.desc)
	}
}

// settingsPane is a modal overlay for toggling overview Prefs. The pane holds
// a local snapshot (backing fields) and emits agentsSettingsToggleMsg on each
// flip; the parent model owns Prefs persistence so we don't keep a pointer
// into a bubbletea value-receiver model that may be copied across Updates.
type settingsPane struct {
	open    bool
	cursor  int
	filter  textinput.Model
	matches []fuzzy.Match
	prefs   Prefs // local snapshot, seeded by show(prefs)
}

func newSettingsPane() *settingsPane {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Prompt = "› "
	ti.CharLimit = 64
	p := &settingsPane{filter: ti}
	p.recomputeMatches()
	return p
}

func (p *settingsPane) isOpen() bool { return p != nil && p.open }

// show reseeds the pane snapshot from the parent's current prefs and opens it.
func (p *settingsPane) show(prefs Prefs) {
	if p == nil {
		return
	}
	p.open = true
	p.cursor = 0
	p.filter.SetValue("")
	p.filter.Focus()
	p.prefs = prefs
	p.recomputeMatches()
}

func (p *settingsPane) close() {
	if p == nil {
		return
	}
	p.open = false
}

func (p *settingsPane) recomputeMatches() {
	p.matches = tui.FilterRows(p.filter.Value(), settingsHayAgents)
	if p.cursor >= len(p.matches) || p.cursor < 0 {
		p.cursor = 0
	}
}

// agentsSettingsToggleMsg announces a toggle so the parent can persist it
// and mirror into its own Prefs.
type agentsSettingsToggleMsg struct {
	key   string
	value bool
}

// update routes key events while open. Returns an optional cmd
// (only fires agentsSettingsToggleMsg on flips).
func (p *settingsPane) update(msg tea.Msg) tea.Cmd {
	if !p.isOpen() {
		return nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch km.String() {
	case "esc", "ctrl+c":
		p.open = false
		return nil
	case "down", "ctrl+n":
		if p.cursor < len(p.matches)-1 {
			p.cursor++
		}
		return nil
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
		return nil
	case "enter", "tab", " ":
		return p.toggleCurrent()
	}
	prev := p.filter.Value()
	var cmd tea.Cmd
	p.filter, cmd = p.filter.Update(km)
	if p.filter.Value() != prev {
		p.cursor = 0
		p.recomputeMatches()
	}
	return cmd
}

func (p *settingsPane) toggleCurrent() tea.Cmd {
	if len(p.matches) == 0 {
		return nil
	}
	idx := p.matches[p.cursor].Index
	if idx < 0 || idx >= len(settingsRowsAgents) {
		return nil
	}
	row := settingsRowsAgents[idx]
	f := p.prefs.boolPref(row.key)
	*f = !*f
	newVal := *f
	return func() tea.Msg { return agentsSettingsToggleMsg{key: row.key, value: newVal} }
}

// applyPrefToggle mirrors a key/value into a Prefs in place. Used by the
// parent model on agentsSettingsToggleMsg so the renderer reflects the flip
// immediately even though Persist runs async via tui.PersistSetting.
func applyPrefToggle(p *Prefs, key string, v bool) {
	if f := p.boolPref(key); f != nil {
		*f = v
	}
}

// persist writes one key (toggle, default model / provider) to
// ~/.bee/config.toml.
func persist(key string, value any) error {
	return tui.PersistSetting("", key, value)
}

// view renders the modal overlay. Width/height come from the model's frame.
func (p *settingsPane) view(width, height int) string {
	if !p.isOpen() {
		return ""
	}
	title := titleStyle.Render("⬢ Agents overview — settings")
	hl := lipgloss.NewStyle().Foreground(honey).Bold(true)

	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString(p.filter.View())
	b.WriteString("\n\n")

	if len(p.matches) == 0 {
		b.WriteString(dimStyle.Render("  no matches"))
		b.WriteString("\n")
	}

	for i, m := range p.matches {
		r := settingsRowsAgents[m.Index]
		marker := "  "
		nameStyle := bodyStyle
		if i == p.cursor {
			marker = cursorStyle.Render("▸ ")
			nameStyle = lipgloss.NewStyle().Foreground(honey).Bold(true)
		}
		toggle := "[ ]"
		if *p.prefs.boolPref(r.key) {
			toggle = goodStyle.Render("[x]")
		}
		b.WriteString(marker)
		b.WriteString(toggle)
		b.WriteString("  ")
		b.WriteString(tui.HighlightLabel(r.label, m.MatchedIndexes, nameStyle, hl, 22))
		b.WriteString("  ")
		b.WriteString(dimStyle.Render(r.desc))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("type filter · ↑/↓ pick · enter/space toggle · esc close · saved to ~/.bee/config.toml"))

	return overlayBox(b.String(), width, height)
}

func overlayBox(body string, width, height int) string {
	if width < 40 {
		width = 40
	}
	if height < 10 {
		height = 10
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(honey).
		Padding(0, 1).
		Width(width - 4).
		Height(height - 2).
		Render(body)
}
