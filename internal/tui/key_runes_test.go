package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// bubbletea hands over a word that arrived in one read as a single runes
// message whose String() is the word, so "up" would press the up key and
// recall history. Fed a rune at a time the text lands intact.
func TestTypedWordsThatSpellKeyNamesStayText(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BEE_HOME", dir)
	if err := os.WriteFile(filepath.Join(dir, "history"), []byte("/status\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t)
	// the words arrive back to back, each as its own runes message
	for _, chunk := range []string{"clean", " ", "up", " ", "down", " ", "enter", " ", "tab", " ", "left"} {
		var nm tea.Model
		if chunk == " " {
			nm, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
		} else {
			nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(chunk)})
		}
		m = nm.(Model)
	}
	if got := m.input.Value(); got != "clean up down enter tab left" {
		t.Fatalf("input = %q", got)
	}
	if m.cycleActive {
		t.Fatal("the word up started history cycling")
	}
}

func TestSplitRunes(t *testing.T) {
	keys := splitRunes([]rune("héy"))
	if len(keys) != 3 || string(keys[1].Runes) != "é" || keys[2].Type != tea.KeyRunes {
		t.Fatalf("got %+v", keys)
	}
}

// a bracketed paste is one atomic insert, never split
func TestBracketedPasteStaysWhole(t *testing.T) {
	m := newTestModel(t)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("up the demo"), Paste: true})
	if got := nm.(Model).input.Value(); got != "up the demo" {
		t.Fatalf("paste split or mangled: %q", got)
	}
}
