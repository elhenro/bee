package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
)

func TestAtPicker_FilterMatches(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "internal", "tui"), 0o755)
	os.WriteFile(filepath.Join(dir, "internal", "tui", "app.go"), nil, 0o644)
	p := NewAtPicker(dir)
	if len(p.matches) == 0 {
		t.Fatal("want some initial matches")
	}
}

func atPickAt(t *testing.T, value string, ups, lefts int) Model {
	t.Helper()
	m := NewModel(nil, "/tmp/work", "m", "", caveman.Default)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(Model)
	m.input.SetValue(value)
	press := func(k tea.KeyMsg) {
		nm, _ := m.Update(k)
		m = nm.(Model)
	}
	for i := 0; i < ups; i++ {
		press(tea.KeyMsg{Type: tea.KeyUp})
	}
	if ups > 0 {
		m.input.CursorEnd()
	}
	for i := 0; i < lefts; i++ {
		press(tea.KeyMsg{Type: tea.KeyLeft})
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("@")})
	if !m.atpicker.Active {
		t.Fatal("@ did not open the picker")
	}
	m.atpicker.Active = false // the picker closes itself before it emits the pick
	nm, _ = m.Update(AtPickerSelectMsg{Path: "main.go"})
	return nm.(Model)
}

// picking a file replaces only the "@" it was opened with; text after the
// cursor and on later lines stays.
func TestAtPicker_SelectKeepsTextAfterCursor(t *testing.T) {
	cases := []struct {
		name       string
		value      string
		ups, lefts int
		want       string
	}{
		{"end of line", "look at ", 0, 0, "look at main.go"},
		{"mid line", "foo bar", 0, 3, "foo main.gobar"},
		{"earlier line", "fix it in \nsecond line\nthird line", 2, 0, "fix it in main.go\nsecond line\nthird line"},
	}
	for _, c := range cases {
		m := atPickAt(t, c.value, c.ups, c.lefts)
		if got := m.input.Value(); got != c.want {
			t.Errorf("%s: value = %q, want %q", c.name, got, c.want)
		}
		// cursor lands right after the inserted path
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
		if got := nm.(Model).input.Value(); !strings.Contains(got, "main.go!") {
			t.Errorf("%s: cursor not after path: %q", c.name, got)
		}
	}
}
