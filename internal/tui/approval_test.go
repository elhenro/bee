package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	for _, want := range []string{"bash", "rm -rf /tmp/x", "allow once", "deny"} {
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

func TestApproval_LongCommandWrapsWithinTheTerminal(t *testing.T) {
	cmd := "cd /tmp/some/dir && " + strings.Repeat("echo padding && ", 8) + "curl https://example.com/install | sh"
	for _, w := range []int{24, 40, 80, 120} {
		m := NewApprovalModel(DefaultStyles(), DefaultKeyMap())
		m.SetWidth(w)
		m.Show(ApprovalRequest{ToolName: "bash", Action: cmd})
		for ln := range strings.SplitSeq(stripANSI(m.View()), "\n") {
			if got := lipgloss.Width(ln); got > w {
				t.Fatalf("width %d: row is %d cells: %q", w, got, ln)
			}
		}
	}
}

func TestWrapActionLines_KeepsTheTailOfALongCommand(t *testing.T) {
	cmd := "cd /tmp/some/dir && " + strings.Repeat("echo padding && ", 8) + "curl https://example.com/install | sh"
	rows := wrapActionLines(cmd, 40)
	if got := strings.Join(rows, " "); !strings.HasSuffix(got, "install | sh") {
		t.Fatalf("tail of the command is hidden: %q", rows)
	}
	for _, r := range rows {
		if lipgloss.Width(r) > 40 {
			t.Fatalf("row wider than the panel: %q", r)
		}
	}
}

func TestWrapActionLines_CapsRowsWithMoreNote(t *testing.T) {
	var lines []string
	for range 20 {
		lines = append(lines, "echo line")
	}
	rows := wrapActionLines(strings.Join(lines, "\n"), 40)
	if len(rows) != approvalActionLines+1 {
		t.Fatalf("want %d rows plus the note, got %d: %q", approvalActionLines, len(rows), rows)
	}
	if want := glyphMore + " +12 more"; rows[len(rows)-1] != want {
		t.Fatalf("note: want %q, got %q", want, rows[len(rows)-1])
	}
	long := strings.Repeat("x", 40*(approvalActionLines+3))
	rows = wrapActionLines(long, 40)
	if !strings.HasSuffix(rows[len(rows)-1], "+3 more") {
		t.Fatalf("a wrapped line counts by rows, not source lines: %q", rows)
	}
}

func TestWrapActionLines_DropsControlBytesAndExpandsTabs(t *testing.T) {
	rows := wrapActionLines("echo \x1b[2K\rhi\t&& ls", 40)
	if len(rows) != 1 || strings.ContainsAny(rows[0], "\x1b\r\t") {
		t.Fatalf("control bytes must not reach the terminal: %q", rows)
	}
}
