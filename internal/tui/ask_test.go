package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/ask"
)

func newAskFixture() AskModel {
	m := NewAskModel(DefaultStyles())
	m.Show("ask-1", ask.Question{
		Header: "engine",
		Prompt: "3D engine?",
		Options: []ask.Option{
			{Label: "Three.js"},
			{Label: "Babylon.js", Recommended: true},
		},
		AllowCustom: true,
	})
	return m
}

func TestAsk_ShowFocusesRecommended(t *testing.T) {
	m := newAskFixture()
	if m.focus != 1 {
		t.Fatalf("focus should start on recommended option, got %d", m.focus)
	}
}

func TestAsk_EnterPicksFocused(t *testing.T) {
	m := newAskFixture()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m2.Active {
		t.Fatal("picker should deactivate after pick")
	}
	if cmd == nil {
		t.Fatal("expected answer cmd")
	}
	msg, ok := cmd().(AskAnswerMsg)
	if !ok || msg.UseID != "ask-1" {
		t.Fatalf("expected AskAnswerMsg for ask-1, got %+v", msg)
	}
	if msg.Answer.Index != 1 || msg.Answer.Text != "Babylon.js" {
		t.Fatalf("got %+v, want recommended pick", msg.Answer)
	}
}

func TestAsk_NumberKeyPicks(t *testing.T) {
	m := newAskFixture()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	msg := cmd().(AskAnswerMsg)
	if msg.Answer.Index != 0 || msg.Answer.Text != "Three.js" {
		t.Fatalf("number key 1 should pick first option, got %+v", msg.Answer)
	}
}

func TestAsk_EscDismisses(t *testing.T) {
	m := newAskFixture()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	msg := cmd().(AskAnswerMsg)
	if !msg.Answer.Dismissed {
		t.Fatalf("esc should dismiss, got %+v", msg.Answer)
	}
}

func TestAsk_CustomTextFlow(t *testing.T) {
	m := newAskFixture()
	// down past both options to the custom row, enter to start typing
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // 1 -> 0... wraps; just set focus
	m.focus = m.customIdx()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.typing {
		t.Fatal("enter on custom row should enter typing mode")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("WebGPU")})
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m2.Active {
		t.Fatal("submitting custom text should close picker")
	}
	msg := cmd().(AskAnswerMsg)
	if msg.Answer.Index != -1 || msg.Answer.Text != "WebGPU" {
		t.Fatalf("custom submit got %+v", msg.Answer)
	}
}

func TestAsk_ViewShowsRecommended(t *testing.T) {
	out := stripANSI(newAskFixture().View())
	if !strings.Contains(out, "3D engine?") || !strings.Contains(out, "recommended") {
		t.Fatalf("view missing prompt/recommended marker:\n%s", out)
	}
}
