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

func init() {
	sort.Slice(settingsRowsAgents, func(i, j int) bool {
		return settingsRowsAgents[i].label < settingsRowsAgents[j].label
	})
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
	needle := strings.TrimSpace(p.filter.Value())
	if needle == "" {
		p.matches = make([]fuzzy.Match, len(settingsRowsAgents))
		for i, r := range settingsRowsAgents {
			p.matches[i] = fuzzy.Match{Index: i, Str: r.label}
		}
	} else {
		hay := make([]string, len(settingsRowsAgents))
		for i, r := range settingsRowsAgents {
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
	cur := p.read(row.key)
	newVal := !cur
	p.write(row.key, newVal)
	return func() tea.Msg { return agentsSettingsToggleMsg{key: row.key, value: newVal} }
}

func (p *settingsPane) read(key string) bool {
	switch key {
	case "agents_show_peek":
		return p.prefs.ShowPeek
	case "agents_show_badges":
		return p.prefs.ShowBadges
	case "agents_show_chip":
		return p.prefs.ShowChip
	case "agents_show_subheader":
		return p.prefs.ShowSubheader
	case "agents_show_hint":
		return p.prefs.ShowHint
	case "agents_show_merged":
		return p.prefs.ShowMerged
	}
	return false
}

func (p *settingsPane) write(key string, v bool) {
	switch key {
	case "agents_show_peek":
		p.prefs.ShowPeek = v
	case "agents_show_badges":
		p.prefs.ShowBadges = v
	case "agents_show_chip":
		p.prefs.ShowChip = v
	case "agents_show_subheader":
		p.prefs.ShowSubheader = v
	case "agents_show_hint":
		p.prefs.ShowHint = v
	case "agents_show_merged":
		p.prefs.ShowMerged = v
	}
}

// applyPrefToggle mirrors a key/value into a Prefs in place. Used by the
// parent model on agentsSettingsToggleMsg so the renderer reflects the flip
// immediately even though Persist runs async via tui.PersistSetting.
func applyPrefToggle(p *Prefs, key string, v bool) {
	switch key {
	case "agents_show_peek":
		p.ShowPeek = v
	case "agents_show_badges":
		p.ShowBadges = v
	case "agents_show_chip":
		p.ShowChip = v
	case "agents_show_subheader":
		p.ShowSubheader = v
	case "agents_show_hint":
		p.ShowHint = v
	case "agents_show_merged":
		p.ShowMerged = v
	}
}

// persistToggle writes one row to ~/.bee/config.toml.
func persistToggle(key string, value bool) error {
	return tui.PersistSetting("", key, value)
}

// persistString writes a string-valued pref (default model / provider).
func persistString(key, value string) error {
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
		if p.read(r.key) {
			toggle = goodStyle.Render("[x]")
		}
		b.WriteString(marker)
		b.WriteString(toggle)
		b.WriteString("  ")
		b.WriteString(renderHighlightedLabel(r.label, m.MatchedIndexes, nameStyle, hl, 22))
		b.WriteString("  ")
		b.WriteString(dimStyle.Render(r.desc))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("type filter · ↑/↓ pick · enter/space toggle · esc close · saved to ~/.bee/config.toml"))

	return overlayBox(b.String(), width, height)
}

func renderHighlightedLabel(label string, matched []int, base, hl lipgloss.Style, pad int) string {
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
	if extra := pad - len(label); extra > 0 {
		b.WriteString(strings.Repeat(" ", extra))
	}
	return b.String()
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
		Render(body)
}
