package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCycleEffort(t *testing.T) {
	want := map[string]string{
		"auto": "off", "off": "low", "low": "medium", "medium": "high",
		"high": "max", "max": "auto", "": "off", "med": "high", "bogus": "off",
	}
	for in, out := range want {
		if got := cycleEffort(in); got != out {
			t.Errorf("cycleEffort(%q) = %q, want %q", in, got, out)
		}
	}
}

func TestModel_CtrlTCyclesEffort(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	m := newTestModel(t)
	m.thinking = "auto"
	m.input.SetValue("half typed prompt")
	for _, lvl := range []string{"off", "low"} {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
		m = m2.(Model)
		if m.thinking != lvl {
			t.Fatalf("thinking = %q, want %q", m.thinking, lvl)
		}
	}
	if m.input.Value() != "half typed prompt" {
		t.Errorf("ctrl+t touched the input buffer: %q", m.input.Value())
	}
	if m.warning != "effort: low" {
		t.Errorf("warning = %q", m.warning)
	}
}
