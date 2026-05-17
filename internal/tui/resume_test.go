package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// openWithEntries seeds entries and opens the picker without touching disk —
// the real Show() loads from ~/.bee/sessions which would clobber the fixture.
func openWithEntries(p *ResumePicker, e []ResumeEntry) {
	p.SetEntries(e)
	p.open = true
}

func TestResumePicker_EscClosesAndEmitsDismissed(t *testing.T) {
	p := NewResumePicker()
	openWithEntries(p, []ResumeEntry{{ID: "a", Created: time.Now(), Preview: "hi"}})
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.Open() {
		t.Fatal("esc should close")
	}
	if cmd == nil {
		t.Fatal("esc should emit a cmd")
	}
	if _, ok := cmd().(ResumeDismissedMsg); !ok {
		t.Fatalf("expected ResumeDismissedMsg")
	}
}

func TestResumePicker_ArrowNavAndEnter(t *testing.T) {
	p := NewResumePicker()
	openWithEntries(p, []ResumeEntry{
		{ID: "alpha", Created: time.Now(), Preview: "first prompt"},
		{ID: "beta", Created: time.Now().Add(-time.Hour), Preview: "second"},
		{ID: "gamma", Created: time.Now().Add(-2 * time.Hour), Preview: "third"},
	})
	if p.Selected() != 0 {
		t.Fatalf("selected = %d, want 0", p.Selected())
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if p.Selected() != 2 {
		t.Fatalf("selected = %d, want 2", p.Selected())
	}
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyUp})
	if p.Selected() != 1 {
		t.Fatalf("selected = %d, want 1", p.Selected())
	}
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should emit a cmd")
	}
	sel, ok := cmd().(ResumeSelectMsg)
	if !ok {
		t.Fatalf("expected ResumeSelectMsg")
	}
	if sel.ID != "beta" {
		t.Errorf("selected id = %q, want beta", sel.ID)
	}
}

func TestResumePicker_ViewContainsPreviewAndAge(t *testing.T) {
	p := NewResumePicker()
	openWithEntries(p, []ResumeEntry{
		{ID: "abc12345", Created: time.Now().Add(-30 * time.Minute), Preview: "fix the bug"},
	})
	out := p.View(80, 24)
	for _, want := range []string{"abc12345", "fix the bug", "m ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q in:\n%s", want, out)
		}
	}
}

func TestResumePicker_EmptyShowsHint(t *testing.T) {
	p := NewResumePicker()
	openWithEntries(p, nil)
	out := p.View(60, 12)
	if !strings.Contains(out, "no past sessions") {
		t.Errorf("expected empty hint, got:\n%s", out)
	}
}
