package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/commands"
	"github.com/elhenro/bee/internal/skills"
)

// fakeSkills is a stub SkillsLister for tests.
type fakeSkills struct{ list []skills.Skill }

func (f *fakeSkills) List() []skills.Skill { return f.list }

func (f *fakeSkills) Get(name string) (skills.Skill, bool) {
	for _, s := range f.list {
		if s.Name == name {
			return s, true
		}
	}
	return skills.Skill{}, false
}

func newTestPalette(t *testing.T) PaletteModel {
	t.Helper()
	t.Setenv("BEE_HOME", t.TempDir())
	reg := commands.NewRegistry()
	commands.RegisterBuiltins(reg)
	sk := &fakeSkills{list: []skills.Skill{
		{Name: "calc", Description: "one-shot commit flow"},
		{Name: "release", Description: "deploy pipeline"},
		{Name: "hermes", Description: "personal life agent"},
	}}
	return NewPalette(reg, sk)
}

func newPaletteCmdsOnly(t *testing.T) PaletteModel {
	t.Helper()
	t.Setenv("BEE_HOME", t.TempDir())
	reg := commands.NewRegistry()
	commands.RegisterBuiltins(reg)
	p := NewPalette(reg, nil)
	p.Show("")
	return p
}

func TestPalette_PoolMergesCommandsAndSkills(t *testing.T) {
	p := newTestPalette(t)
	p.Show("")
	if len(p.pool) == 0 {
		t.Fatal("empty pool")
	}
	var foundCmd, foundSkill bool
	for _, e := range p.pool {
		if e.Kind == EntryCommand && e.Name == "help" {
			foundCmd = true
		}
		if e.Kind == EntrySkill && e.Name == "calc" {
			foundSkill = true
		}
	}
	if !foundCmd || !foundSkill {
		t.Errorf("missing entries: cmd=%v skill=%v", foundCmd, foundSkill)
	}
}

func TestPalette_FuzzyRanking(t *testing.T) {
	p := newTestPalette(t)
	p.Show("cmpt") // should rank /compact highly
	if len(p.matches) == 0 {
		t.Fatal("no matches for cmpt")
	}
	top := p.pool[p.matches[0].Index]
	if top.Name != "compact" {
		t.Errorf("want compact first, got %q (matches=%d)", top.Name, len(p.matches))
	}
}

func TestPalette_FuzzyMatchesSkill(t *testing.T) {
	p := newTestPalette(t)
	p.Show("rls")
	if len(p.matches) == 0 {
		t.Fatal("no matches for rls")
	}
	found := false
	for _, m := range p.matches {
		if p.pool[m.Index].Name == "release" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected release in matches for rls")
	}
}

func TestPalette_ShowWithInitial(t *testing.T) {
	p := newTestPalette(t)
	p.Show("hlp")
	if p.input.Value() != "hlp" {
		t.Errorf("initial value should pre-fill, got %q", p.input.Value())
	}
	if len(p.matches) == 0 {
		t.Fatal("expected matches for hlp")
	}
	top := p.pool[p.matches[0].Index]
	if top.Name != "help" {
		t.Errorf("want help top, got %q", top.Name)
	}
}

func TestPalette_EmptyInputShowsAll(t *testing.T) {
	p := newTestPalette(t)
	p.Show("")
	if len(p.matches) != len(p.pool) {
		t.Errorf("empty input should match all, got %d/%d", len(p.matches), len(p.pool))
	}
}

func TestPalette_View_HasHighlight(t *testing.T) {
	p := newTestPalette(t)
	p.Show("hlp")
	v := p.View()
	if !strings.Contains(v, "help") {
		t.Errorf("view should mention help, got: %s", v)
	}
	// command rows are prefixed with "/" glyph; help is a command.
	if !strings.Contains(v, "/help") {
		t.Errorf("view should prefix command with /, got: %s", v)
	}
}

func TestPalette_View_SkillsGlyph(t *testing.T) {
	p := newTestPalette(t)
	p.Show("calc")
	v := p.View()
	// skill rows are prefixed with "#" glyph.
	if !strings.Contains(v, "#calc") {
		t.Errorf("view should prefix skill with #, got: %s", v)
	}
}

func TestPalette_View_MatchedIndexesMaskedToName(t *testing.T) {
	// regression: highlight indices from fuzzy.Find are positions in
	// "name description"; the renderer must mask them to the name range
	// before indexing into Name to avoid out-of-range panics or stray
	// highlights spilling into the description column.
	p := newTestPalette(t)
	p.Show("clone session") // matches "clone" command (name+desc fuzzy)
	v := p.View()
	if !strings.Contains(v, "clone") {
		t.Errorf("view should contain clone, got: %s", v)
	}
}

func TestPalette_Esc_Dismisses(t *testing.T) {
	p := newPaletteCmdsOnly(t)
	p2, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p2.Active {
		t.Error("esc should clear Active")
	}
	if cmd == nil {
		t.Fatal("esc should emit dismiss cmd")
	}
	if _, ok := cmd().(PaletteDismissedMsg); !ok {
		t.Fatalf("want PaletteDismissedMsg, got %T", cmd())
	}
}

func TestPalette_Enter_EmitsSelect(t *testing.T) {
	p := newPaletteCmdsOnly(t)
	p.input.SetValue("help") // filter narrows
	p.recomputeMatches()
	p2, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p2.Active {
		t.Error("enter should clear Active")
	}
	if cmd == nil {
		t.Fatal("expected select cmd")
	}
	msg, ok := cmd().(PaletteSelectMsg)
	if !ok {
		t.Fatalf("want PaletteSelectMsg, got %T", cmd())
	}
	if msg.Name != "help" {
		t.Errorf("want help, got %q", msg.Name)
	}
	if msg.Kind != EntryCommand {
		t.Errorf("want EntryCommand, got %v", msg.Kind)
	}
}

func TestPalette_Enter_SkillSelectKind(t *testing.T) {
	p := newTestPalette(t)
	p.Show("calc")
	if len(p.matches) == 0 {
		t.Fatal("no matches for calc")
	}
	// top of the matches should be the calc skill
	p2, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p2.Active {
		t.Error("enter should clear Active")
	}
	msg, ok := cmd().(PaletteSelectMsg)
	if !ok {
		t.Fatalf("want PaletteSelectMsg, got %T", cmd())
	}
	if msg.Name != "calc" || msg.Kind != EntrySkill {
		t.Errorf("want calc/skill, got %+v", msg)
	}
}

func TestPalette_DownUp_MovesSelection(t *testing.T) {
	p := newPaletteCmdsOnly(t)
	p2, _ := p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p2.selected != 1 {
		t.Errorf("down: want sel=1, got %d", p2.selected)
	}
	p3, _ := p2.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p3.selected != 0 {
		t.Errorf("up: want sel=0, got %d", p3.selected)
	}
	// up at 0 should not go negative
	p4, _ := p3.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p4.selected != 0 {
		t.Errorf("up at 0: want 0, got %d", p4.selected)
	}
}

func TestPalette_Enter_NoMatchNoCmd(t *testing.T) {
	p := newPaletteCmdsOnly(t)
	p.input.SetValue("zzz-no-match")
	p.recomputeMatches()
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("enter with empty filter result should produce no cmd")
	}
}

func TestPalette_View_RendersListWhenActive(t *testing.T) {
	p := newPaletteCmdsOnly(t)
	p.SetWidth(120)
	out := p.View()
	// first-page rows: alphabetized commands well inside maxPaletteRows.
	for _, want := range []string{"compact", "clear"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q: %q", want, out)
		}
	}
	// remainder collapses into a "+N more" overflow footer rather than
	// growing the strip indefinitely.
	if !strings.Contains(out, "more") {
		t.Errorf("expected overflow footer, got %q", out)
	}
}

func TestPalette_View_EmptyWhenInactive(t *testing.T) {
	r := commands.NewRegistry()
	commands.RegisterBuiltins(r)
	p := NewPalette(r, nil) // not active
	if p.View() != "" {
		t.Errorf("inactive palette should render empty, got %q", p.View())
	}
}

func TestPalette_TypingResetsSelection(t *testing.T) {
	p := newPaletteCmdsOnly(t)
	p2, _ := p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p2.selected != 1 {
		t.Fatalf("setup: want sel=1, got %d", p2.selected)
	}
	// type "h" — selection should reset
	p3, _ := p2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if p3.selected != 0 {
		t.Errorf("typing should reset selection to 0, got %d", p3.selected)
	}
}

func TestPalette_NilSkillsLister_OK(t *testing.T) {
	p := newPaletteCmdsOnly(t)
	if len(p.pool) == 0 {
		t.Fatal("pool should have commands even with nil skills")
	}
	for _, e := range p.pool {
		if e.Kind == EntrySkill {
			t.Errorf("found skill entry with nil skills source: %+v", e)
		}
	}
}

func TestPalette_View_SelectionScrollsWindow(t *testing.T) {
	// regression: when selection moves past maxPaletteRows, the visible
	// window must slide so the highlighted row stays in view. previously
	// the renderer pinned to rows[:maxPaletteRows] regardless of selected.
	p := newPaletteCmdsOnly(t)
	p.SetWidth(120)
	if len(p.matches) <= maxPaletteRows {
		t.Skipf("need >%d entries to exercise scrolling, have %d", maxPaletteRows, len(p.matches))
	}
	// move selection to the last entry
	target := len(p.matches) - 1
	for i := 0; i < target; i++ {
		p2, _ := p.Update(tea.KeyMsg{Type: tea.KeyDown})
		p = p2
	}
	if p.selected != target {
		t.Fatalf("setup: want sel=%d, got %d", target, p.selected)
	}
	out := p.View()
	lastName := p.pool[p.matches[target].Index].Name
	if !strings.Contains(out, lastName) {
		t.Errorf("last entry %q must be visible when selected, got: %s", lastName, out)
	}
	// upward-overflow marker should appear since we scrolled past the top
	if !strings.Contains(out, "↑") {
		t.Errorf("expected up-overflow marker, got: %s", out)
	}
}

func TestPalette_View_NoMatchesShowsMessage(t *testing.T) {
	p := newTestPalette(t)
	p.Show("zzzzzzzzzz-impossible")
	v := p.View()
	if !strings.Contains(v, "no matches") {
		t.Errorf("expected 'no matches' marker, got: %q", v)
	}
}
