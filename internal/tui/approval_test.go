package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newApprovalFixture() ApprovalModel {
	m := NewApprovalModel(DefaultStyles(), DefaultKeyMap())
	m.Show(ApprovalRequest{
		ToolName: "bash",
		Action:   "rm -rf /tmp/x",
		UseID:    "u-42",
	})
	return m
}

func TestApproval_AllowKey(t *testing.T) {
	m := newApprovalFixture()
	out := make(chan ApprovalDecisionMsg, 1)
	m.SetOutput(out)

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("expected a decision cmd")
	}
	if m2.Active {
		t.Fatal("expected modal to deactivate")
	}
	msg := cmd()
	dec, ok := msg.(ApprovalDecisionMsg)
	if !ok {
		t.Fatalf("expected ApprovalDecisionMsg, got %T", msg)
	}
	if dec.Decision != ApprovalAllow || dec.UseID != "u-42" {
		t.Fatalf("got %+v", dec)
	}
	select {
	case got := <-out:
		if got.Decision != ApprovalAllow {
			t.Fatalf("channel got %+v", got)
		}
	default:
		t.Fatal("expected channel publish")
	}
}

func TestApproval_DenyKey(t *testing.T) {
	m := newApprovalFixture()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd == nil {
		t.Fatal("expected cmd")
	}
	if m2.Active {
		t.Fatal("expected modal off after deny")
	}
	dec := cmd().(ApprovalDecisionMsg)
	if dec.Decision != ApprovalDeny {
		t.Fatalf("want deny, got %+v", dec)
	}
}

func TestApproval_TabCyclesFocus(t *testing.T) {
	m := newApprovalFixture()
	if m.focus != 0 {
		t.Fatalf("expected focus=0 (allow), got %d", m.focus)
	}
	for want := 1; want < 4; want++ {
		m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		if m2.focus != want {
			t.Fatalf("after %d tabs focus=%d, want %d", want, m2.focus, want)
		}
		m = m2
	}
	// Wrap back to 0.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m2.focus != 0 {
		t.Fatalf("expected wrap to 0, got %d", m2.focus)
	}
}

func TestApproval_SessionKey(t *testing.T) {
	m := newApprovalFixture()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	dec := cmd().(ApprovalDecisionMsg)
	if dec.Decision != ApprovalSession {
		t.Fatalf("got %+v", dec)
	}
}

func TestApproval_AlwaysKey(t *testing.T) {
	m := newApprovalFixture()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	dec := cmd().(ApprovalDecisionMsg)
	if dec.Decision != ApprovalAlways {
		t.Fatalf("got %+v", dec)
	}
}

func TestApproval_EnterSubmitsFocused(t *testing.T) {
	m := newApprovalFixture()
	// Cycle to "session" (focus=1) then press enter.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dec := cmd().(ApprovalDecisionMsg)
	if dec.Decision != ApprovalSession {
		t.Fatalf("enter on focus=1 should pick session, got %+v", dec)
	}
}

func TestApproval_ViewRendersAction(t *testing.T) {
	m := newApprovalFixture()
	out := stripANSI(m.View())
	for _, want := range []string{"permission request", "bash", "rm -rf /tmp/x", "[a]", "[d]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view missing %q: %q", want, out)
		}
	}
}

func TestApproval_InactiveViewEmpty(t *testing.T) {
	m := NewApprovalModel(DefaultStyles(), DefaultKeyMap())
	if got := m.View(); got != "" {
		t.Fatalf("inactive view should be empty, got %q", got)
	}
}
