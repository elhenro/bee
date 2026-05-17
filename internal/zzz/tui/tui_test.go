package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elhenro/bee/internal/zzz"
)

func TestBeeFrameCycle(t *testing.T) {
	// every frame must yield 3 non-empty rows. ensures the slice was wired up.
	for i := 0; i < len(beeFrames)*2; i++ {
		top, mid, bot := BeeFrame(i)
		if top == "" || mid == "" || bot == "" {
			t.Fatalf("frame %d returned empty rows: %q %q %q", i, top, mid, bot)
		}
	}
	if BeeWidth() <= 0 {
		t.Fatal("BeeWidth should be positive")
	}
}

func TestPhaseGlyphs(t *testing.T) {
	cases := map[string]bool{
		"committed": true,
		"noop":      true,
		"failed":    true,
		"running":   true,
	}
	for status := range cases {
		if got := phaseGlyph(status); got == "·" {
			t.Errorf("phaseGlyph(%q) returned fallback dot", status)
		}
	}
}

func TestModelImplementsUI(t *testing.T) {
	var _ zzz.UI = (*Model)(nil)
	var _ zzz.Steerable = (*Model)(nil)
}

func TestModelDispatchsSlashCommands(t *testing.T) {
	run := &zzz.Run{ID: "test", Branch: "zzz/test"}
	m := New(run, zzz.Config{})
	// simulate window size so View doesn't bail on width==0
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = upd.(*Model)

	m.input.SetValue("/stop")
	if _, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}); true {
	}
	select {
	case s := <-m.Steer():
		if s.Kind != zzz.SteerStop {
			t.Fatalf("want SteerStop, got %q", s.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("no steer msg after /stop")
	}

	m.input.SetValue("ship it")
	if _, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}); true {
	}
	select {
	case s := <-m.Steer():
		if s.Kind != zzz.SteerNote || s.Text != "ship it" {
			t.Fatalf("want SteerNote 'ship it', got %+v", s)
		}
	case <-time.After(time.Second):
		t.Fatal("no steer msg after free text")
	}
}

func TestModelViewRendersBee(t *testing.T) {
	run := &zzz.Run{ID: "test", Branch: "zzz/test", Status: "running"}
	m := New(run, zzz.Config{MaxIterations: 50})
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = upd.(*Model)
	m.SetIter(2, 50)
	m.SetPhase("commit")
	m.SetTokens(zzz.TokenStat{Input: 1234, Output: 567, USD: 0.0123})
	// drain channel so live state syncs into the model fields
	for i := 0; i < 3; i++ {
		select {
		case msg := <-m.msgs:
			u, _ := m.Update(msg)
			m = u.(*Model)
		default:
		}
	}
	out := m.View()
	if !strings.Contains(out, "bee zzz") {
		t.Errorf("view missing header: %q", out)
	}
	if !strings.Contains(out, "ζ(") {
		t.Errorf("view missing sleeping bee glyph: %q", out)
	}
}
