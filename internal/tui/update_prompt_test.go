package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/update"
)

func TestUpdatePrompt_DecisionRouting(t *testing.T) {
	cases := []struct {
		key  string
		want UpdateDecision
	}{
		{"1", UpdateLater},
		{"2", UpdateNow},
		{"u", UpdateNow},
		{"3", UpdateAlways},
		{"a", UpdateAlways},
		{"4", UpdateNeverAsk},
		{"n", UpdateNeverAsk},
		{"esc", UpdateLater},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			p := NewUpdatePrompt(DefaultStyles())
			p.Show(update.Info{LatestSHA: "abcdef0123", ShortSHA: "abcdef0", Ahead: 1})
			_, cmd := p.Update(tea.KeyMsg{Type: keyTypeForString(tc.key), Runes: []rune(tc.key)})
			if cmd == nil {
				t.Fatalf("no cmd for key %q", tc.key)
			}
			msg, ok := cmd().(updateDecisionMsg)
			if !ok {
				t.Fatalf("want updateDecisionMsg; got %T", cmd())
			}
			if msg.Decision != tc.want {
				t.Errorf("key %q: got %v, want %v", tc.key, msg.Decision, tc.want)
			}
		})
	}
}

func TestUpdatePrompt_TabCyclesFocus(t *testing.T) {
	p := NewUpdatePrompt(DefaultStyles())
	p.Show(update.Info{ShortSHA: "abc1234", Ahead: 1})
	if p.focus != 1 { // default = "update now"
		t.Fatalf("initial focus = %d, want 1", p.focus)
	}
	for i := 0; i < 4; i++ {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if p.focus != 1 {
		t.Fatalf("after 4 tabs focus = %d, want 1 (cycle)", p.focus)
	}
}

// keyTypeForString maps a few of the simple keystrings the test uses back to
// tea.KeyType so KeyMsg.String() returns the expected literal.
func keyTypeForString(s string) tea.KeyType {
	switch s {
	case "esc":
		return tea.KeyEsc
	case "enter":
		return tea.KeyEnter
	case "tab":
		return tea.KeyTab
	}
	return tea.KeyRunes
}
