package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/caveman"
)

// "up, enter, up" must offer the prompt just sent, not the one before it: the
// cycle list is stale once a submit has landed.
func TestHistoryCycle_ResetsAfterSubmit(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	AppendHistory("first prompt")
	AppendHistory("second prompt")
	m := NewModel(nil, "/tmp/work", "m", "", caveman.Default)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = nm.(Model)

	press := func(k tea.KeyType) {
		nm, cmd := m.Update(tea.KeyMsg{Type: k})
		m = nm.(Model)
		// no engine: the submit comes back as a done msg, feed it so the model is idle again
		if cmd == nil {
			return
		}
		if b, ok := cmd().(tea.BatchMsg); ok {
			for _, c := range b {
				if c == nil {
					continue
				}
				if td, ok := c().(turnDoneMsg); ok {
					nm, _ := m.Update(td)
					m = nm.(Model)
				}
			}
		}
	}
	press(tea.KeyUp)
	if got := m.input.Value(); got != "second prompt" {
		t.Fatalf("first up = %q", got)
	}
	press(tea.KeyEnter)
	press(tea.KeyUp)
	if got := m.input.Value(); got != "second prompt" {
		t.Fatalf("up after submit = %q, want the newest prompt", got)
	}
}
