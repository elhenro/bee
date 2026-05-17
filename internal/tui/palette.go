package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/skills"
)

// PaletteEntryKind distinguishes between a slash command and a skill in the
// merged palette pool.
type PaletteEntryKind int

const (
	EntryCommand PaletteEntryKind = iota
	EntrySkill
)

// PaletteEntry is one row in the palette: either a slash command or a skill.
type PaletteEntry struct {
	Name        string
	Description string
	Kind        PaletteEntryKind
}

// PaletteSelectMsg carries the chosen entry. Parent decides how to dispatch
// (command vs skill).
type PaletteSelectMsg struct {
	Name string
	Kind PaletteEntryKind
}

// PaletteDismissedMsg is emitted when the user presses esc.
type PaletteDismissedMsg struct{}

// SkillsLister is the seam the palette uses to fetch skills. Tests inject a
// stub; *skills.Registry already satisfies it. Get is also used by the slash
// dispatcher to resolve "/<name>" against the skill registry when no built-in
// command matches.
type SkillsLister interface {
	List() []skills.Skill
	Get(name string) (skills.Skill, bool)
}

// PaletteModel is the fzf-style picker. It merges slash commands and skills
// into one pool, fuzzy-matches against typed input, and renders highlighted
// matched runes.
type PaletteModel struct {
	Active   bool
	input    textinput.Model
	cmds     *commands.Registry
	skills   SkillsLister
	selected int
	width    int

	// cached entry pool (rebuilt when palette is shown so it's fresh)
	pool []PaletteEntry
	// cached match indices into pool, in rank order
	matches []fuzzy.Match
}

// SetWidth tells the palette how wide a row may be. Set from the app's
// WindowSizeMsg handler so rows truncate cleanly on narrow layouts.
func (p *PaletteModel) SetWidth(w int) { p.width = w }

// NewPalette returns an inactive palette bound to the given registry and
// optional skills source. Either side may be nil.
func NewPalette(cmds *commands.Registry, sk SkillsLister) PaletteModel {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Prompt = "› "
	ti.CharLimit = 64
	ti.Focus()
	return PaletteModel{input: ti, cmds: cmds, skills: sk}
}

// Show activates the palette, rebuilds the entry pool, and applies an
// initial filter. The initial string pre-fills the filter — used when the
// user typed "/hlp" in the main bar and we open the palette pre-filtered.
func (p *PaletteModel) Show(initial string) {
	p.Active = true
	p.selected = 0
	p.input.SetValue(initial)
	p.input.CursorEnd()
	p.input.Focus()
	p.rebuildPool()
	p.recomputeMatches()
}

// SetFilter mirrors the filter from the main input bar — typing lives there
// now, palette just shows the ranked list. Resets selection on change.
func (p *PaletteModel) SetFilter(s string) {
	if p.input.Value() == s {
		return
	}
	p.input.SetValue(s)
	p.selected = 0
	if len(p.pool) == 0 {
		p.rebuildPool()
	}
	p.recomputeMatches()
}

// rebuildPool merges commands and skills into a single entry list. Commands
// come first, then skills — fuzzy ranking takes over for non-empty input.
func (p *PaletteModel) rebuildPool() {
	p.pool = p.pool[:0]
	if p.cmds != nil {
		for _, c := range p.cmds.List() {
			p.pool = append(p.pool, PaletteEntry{Name: c.Name, Description: c.Description, Kind: EntryCommand})
		}
	}
	if p.skills != nil {
		for _, s := range p.skills.List() {
			p.pool = append(p.pool, PaletteEntry{Name: s.Name, Description: s.Description, Kind: EntrySkill})
		}
	}
}

// recomputeMatches runs fuzzy.Find against the current input. With empty
// input we synthesize a match list preserving pool order so the renderer
// stays simple (single code path).
func (p *PaletteModel) recomputeMatches() {
	needle := strings.TrimSpace(p.input.Value())
	if needle == "" {
		p.matches = make([]fuzzy.Match, len(p.pool))
		for i, e := range p.pool {
			p.matches[i] = fuzzy.Match{Index: i, Str: e.Name}
		}
		if p.selected >= len(p.matches) {
			p.selected = 0
		}
		return
	}
	// match against "name description" so descriptions can rank too; the
	// renderer masks highlight indices to the name range below.
	hay := make([]string, len(p.pool))
	for i, e := range p.pool {
		hay[i] = e.Name + " " + e.Description
	}
	p.matches = fuzzy.Find(needle, hay)
	if p.selected >= len(p.matches) {
		p.selected = 0
	}
}

// Update handles palette key events. Returns the updated palette and an
// optional cmd to forward (PaletteSelectMsg or PaletteDismissedMsg).
func (p PaletteModel) Update(msg tea.Msg) (PaletteModel, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc", "ctrl+c":
			p.Active = false
			return p, func() tea.Msg { return PaletteDismissedMsg{} }
		case "enter":
			if len(p.matches) == 0 {
				return p, nil
			}
			idx := p.selected
			if idx < 0 || idx >= len(p.matches) {
				idx = 0
			}
			entry := p.pool[p.matches[idx].Index]
			p.Active = false
			return p, func() tea.Msg { return PaletteSelectMsg{Name: entry.Name, Kind: entry.Kind} }
		case "down", "ctrl+n":
			if p.selected+1 < len(p.matches) {
				p.selected++
			}
			return p, nil
		case "up", "ctrl+p":
			if p.selected > 0 {
				p.selected--
			}
			return p, nil
		}
	}
	prev := p.input.Value()
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(msg)
	if p.input.Value() != prev {
		p.selected = 0
		p.recomputeMatches()
	}
	return p, cmd
}

// maxPaletteRows caps visible rows so the picker never blots out scrollback
// on short terminals. Overflow is summarized with a "+N more" footer.
const maxPaletteRows = 8

// View renders the palette as a borderless, dense strip designed to sit
// directly above the input bar. Width is taken from SetWidth — falls back
// to a sensible default when unset (e.g. tests).
func (p PaletteModel) View() string {
	if !p.Active {
		return ""
	}
	w := p.width
	if w <= 0 {
		w = 80
	}

	dim := lipgloss.NewStyle().Foreground(fgOyster)
	hl := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	cmdGlyph := lipgloss.NewStyle().Foreground(accentYou)
	skillGlyph := lipgloss.NewStyle().Foreground(semSuccess)
	selMark := lipgloss.NewStyle().Foreground(accentHoney).Bold(true)
	nameSel := lipgloss.NewStyle().Foreground(fgButter).Bold(true)
	nameNorm := lipgloss.NewStyle().Foreground(fgSmoke)

	if len(p.matches) == 0 {
		return dim.Render("  no matches")
	}

	total := len(p.matches)
	start := 0
	if total > maxPaletteRows {
		if p.selected >= maxPaletteRows {
			start = p.selected - maxPaletteRows + 1
		}
		if start+maxPaletteRows > total {
			start = total - maxPaletteRows
		}
		if start < 0 {
			start = 0
		}
	}
	end := start + maxPaletteRows
	if end > total {
		end = total
	}
	rows := p.matches[start:end]
	overflowAbove := start
	overflowBelow := total - end

	var b strings.Builder
	if overflowAbove > 0 {
		b.WriteString(dim.Render("  ↑ " + strconv.Itoa(overflowAbove) + " more"))
		b.WriteString("\n")
	}
	for i, m := range rows {
		entry := p.pool[m.Index]
		absIdx := start + i

		var line strings.Builder
		if absIdx == p.selected {
			line.WriteString(selMark.Render("›"))
		} else {
			line.WriteString(" ")
		}
		line.WriteString(" ")

		glyph := "/"
		gs := cmdGlyph
		if entry.Kind == EntrySkill {
			glyph = "#"
			gs = skillGlyph
		}
		line.WriteString(gs.Render(glyph))

		// highlight matched runes within name range only. fuzzy returns
		// indices into "name description"; mask to [0, nameLen).
		nameLen := len(entry.Name)
		matchSet := map[int]struct{}{}
		for _, idx := range m.MatchedIndexes {
			if idx >= 0 && idx < nameLen {
				matchSet[idx] = struct{}{}
			}
		}
		ns := nameNorm
		if absIdx == p.selected {
			ns = nameSel
		}
		for j := 0; j < nameLen; j++ {
			ch := string(entry.Name[j])
			if _, ok := matchSet[j]; ok {
				line.WriteString(hl.Render(ch))
			} else {
				line.WriteString(ns.Render(ch))
			}
		}

		if entry.Description != "" {
			line.WriteString(dim.Render("  " + entry.Description))
		}

		row := line.String()
		if lipglossWidth(row) > w {
			row = truncateVisible(row, w)
		}
		b.WriteString(row)
		if i < len(rows)-1 || overflowBelow > 0 {
			b.WriteString("\n")
		}
	}
	if overflowBelow > 0 {
		b.WriteString(dim.Render("  ↓ " + strconv.Itoa(overflowBelow) + " more"))
	}
	return b.String()
}
