package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newEscalateFixture() EscalateModel {
	m := NewEscalateModel(DefaultStyles())
	m.SetWidth(80)
	m.Show([]string{"adjust test tolerance", "run as root", "different mechanism"})
	return m
}

func TestEscalate_ShowEmptyStaysInactive(t *testing.T) {
	m := NewEscalateModel(DefaultStyles())
	m.Show(nil)
	if m.Active {
		t.Fatal("empty options should not activate the picker")
	}
}

func TestEscalate_EnterPicksFocused(t *testing.T) {
	m := newEscalateFixture()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m2.Active {
		t.Fatal("picker should deactivate after pick")
	}
	msg, ok := cmd().(EscalateChoiceMsg)
	if !ok || msg.Text != "adjust test tolerance" {
		t.Fatalf("got %+v, want first option", msg)
	}
}

func TestEscalate_NumberKeyPicks(t *testing.T) {
	m := newEscalateFixture()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	msg := cmd().(EscalateChoiceMsg)
	if msg.Text != "run as root" {
		t.Fatalf("number key 2 should pick second option, got %q", msg.Text)
	}
}

func TestEscalate_ArrowCyclesAndWraps(t *testing.T) {
	m := newEscalateFixture()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp}) // wrap to last
	if m.focus != 2 {
		t.Fatalf("up from 0 should wrap to last, got %d", m.focus)
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // wrap to first
	if m.focus != 0 {
		t.Fatalf("down from last should wrap to first, got %d", m.focus)
	}
}

func TestEscalate_EscDismissesNoChoice(t *testing.T) {
	m := newEscalateFixture()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m2.Active {
		t.Fatal("esc should deactivate the picker")
	}
	if cmd != nil {
		t.Fatal("esc should not publish a choice")
	}
}

func TestEscalate_ViewListsOptions(t *testing.T) {
	out := stripANSI(newEscalateFixture().View())
	for _, want := range []string{"adjust test tolerance", "run as root", "different mechanism"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing option %q:\n%s", want, out)
		}
	}
}
