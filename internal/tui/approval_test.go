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

func TestApproval_TabTogglesFocus(t *testing.T) {
	m := newApprovalFixture()
	if !m.focusAllow {
		t.Fatal("expected allow to start focused")
	}
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m2.focusAllow {
		t.Fatal("tab should toggle focus")
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
