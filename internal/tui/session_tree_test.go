package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/elhenro/bee/internal/types"
)

func msg(id, parent string, role types.Role, text string) types.Message {
	return types.Message{
		ID:       id,
		ParentID: parent,
		Role:     role,
		Content:  []types.ContentBlock{{Type: types.BlockText, Text: text}},
		Time:     time.Now(),
	}
}

func TestSessionTree_HiddenByDefault(t *testing.T) {
	tree := NewSessionTree()
	if tree.Open() {
		t.Fatal("should start closed")
	}
	if got := tree.View(60, 20); got != "" {
		t.Fatalf("hidden view should be empty, got %q", got)
	}
}

func TestSessionTree_Toggle(t *testing.T) {
	tree := NewSessionTree()
	tree.Update(ToggleSessionTreeMsg{})
	if !tree.Open() {
		t.Fatal("toggle should open")
	}
	tree.Update(ToggleSessionTreeMsg{})
	if tree.Open() {
		t.Fatal("toggle should close")
	}
}

func TestSessionTree_EmptyShowsStub(t *testing.T) {
	tree := NewSessionTree()
	tree.Update(ToggleSessionTreeMsg{})
	out := strip(tree.View(60, 20))
	if !strings.Contains(out, "Session tree") {
		t.Fatalf("missing title, got %q", out)
	}
	if !strings.Contains(out, "no messages") {
		t.Fatalf("missing empty stub, got %q", out)
	}
}

func TestSessionTree_RendersBranches(t *testing.T) {
	msgs := []types.Message{
		msg("a", "", types.RoleUser, "first ask"),
		msg("b", "a", types.RoleAssistant, "first answer"),
		msg("c", "b", types.RoleUser, "main follow up"),
		msg("d", "b", types.RoleUser, "branch follow up"),
	}
	tree := NewSessionTree()
	tree.LoadMessages(msgs, "c")
	tree.Update(ToggleSessionTreeMsg{})
	out := strip(tree.View(80, 24))
	if !strings.Contains(out, "first ask") {
		t.Fatalf("missing root preview: %q", out)
	}
	if !strings.Contains(out, "main follow up") || !strings.Contains(out, "branch follow up") {
		t.Fatalf("expected both branches, got %q", out)
	}
	if !strings.Contains(out, "├─") || !strings.Contains(out, "└─") {
		t.Fatalf("expected branch glyphs, got %q", out)
	}
}

// keyPress builds a tea.KeyMsg for a single rune key.
func keyPress(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func sampleTreeMsgs() []types.Message {
	return []types.Message{
		msg("a", "", types.RoleUser, "first ask"),
		msg("b", "a", types.RoleAssistant, "first answer"),
		msg("c", "b", types.RoleUser, "main follow up"),
		msg("d", "b", types.RoleUser, "branch follow up"),
	}
}

func TestSessionTree_LoadSelectsCurrentLeaf(t *testing.T) {
	tree := NewSessionTree()
	tree.LoadMessages(sampleTreeMsgs(), "c")
	if got := tree.Selected(); got != "c" {
		t.Fatalf("Selected default = %q want %q", got, "c")
	}
}

func TestSessionTree_NavDown(t *testing.T) {
	tree := NewSessionTree()
	tree.LoadMessages(sampleTreeMsgs(), "a") // start at root
	tree.Update(ToggleSessionTreeMsg{})

	tree.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := tree.Selected(); got != "b" {
		t.Fatalf("after Down: selected = %q want b", got)
	}
	tree.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := tree.Selected(); got != "c" {
		t.Fatalf("after Down x2: selected = %q want c", got)
	}
	tree.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := tree.Selected(); got != "d" {
		t.Fatalf("after Down x3: selected = %q want d", got)
	}
	// past end should clamp
	tree.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := tree.Selected(); got != "d" {
		t.Fatalf("clamp at end: selected = %q want d", got)
	}
}

func TestSessionTree_NavUp(t *testing.T) {
	tree := NewSessionTree()
	tree.LoadMessages(sampleTreeMsgs(), "d")
	tree.Update(ToggleSessionTreeMsg{})

	tree.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := tree.Selected(); got != "c" {
		t.Fatalf("after Up: selected = %q want c", got)
	}
	tree.Update(tea.KeyMsg{Type: tea.KeyUp})
	tree.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := tree.Selected(); got != "a" {
		t.Fatalf("after Up to top: selected = %q want a", got)
	}
	tree.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := tree.Selected(); got != "a" {
		t.Fatalf("clamp at start: selected = %q want a", got)
	}
}

func TestSessionTree_EnterEmitsSwitch(t *testing.T) {
	tree := NewSessionTree()
	tree.LoadMessages(sampleTreeMsgs(), "c")
	tree.Update(ToggleSessionTreeMsg{})
	tree.Update(tea.KeyMsg{Type: tea.KeyDown}) // c -> d
	_, cmd := tree.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a cmd on Enter")
	}
	out := cmd()
	sw, ok := out.(SessionSwitchMsg)
	if !ok {
		t.Fatalf("expected SessionSwitchMsg, got %T", out)
	}
	if sw.LeafID != "d" {
		t.Fatalf("LeafID = %q want d", sw.LeafID)
	}
	if tree.Open() {
		t.Fatal("Enter should close modal")
	}
}

func TestSessionTree_ForkKey(t *testing.T) {
	tree := NewSessionTree()
	tree.LoadMessages(sampleTreeMsgs(), "c")
	tree.Update(ToggleSessionTreeMsg{})
	_, cmd := tree.Update(keyPress('f'))
	if cmd == nil {
		t.Fatal("expected cmd on f")
	}
	fm, ok := cmd().(SessionForkMsg)
	if !ok {
		t.Fatalf("expected SessionForkMsg, got %T", cmd())
	}
	if fm.FromID != "c" {
		t.Fatalf("FromID = %q want c", fm.FromID)
	}
	if tree.Open() {
		t.Fatal("f should close modal")
	}
}

func TestSessionTree_CloneKey(t *testing.T) {
	tree := NewSessionTree()
	tree.LoadMessages(sampleTreeMsgs(), "c")
	tree.Update(ToggleSessionTreeMsg{})
	_, cmd := tree.Update(keyPress('c'))
	if cmd == nil {
		t.Fatal("expected cmd on c")
	}
	if _, ok := cmd().(SessionCloneMsg); !ok {
		t.Fatalf("expected SessionCloneMsg, got %T", cmd())
	}
	if tree.Open() {
		t.Fatal("c should close modal")
	}
}

func TestSessionTree_EscCloses(t *testing.T) {
	tree := NewSessionTree()
	tree.LoadMessages(sampleTreeMsgs(), "c")
	tree.Update(ToggleSessionTreeMsg{})
	tree.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tree.Open() {
		t.Fatal("Esc should close modal")
	}
}

func TestSessionTree_CursorRendered(t *testing.T) {
	tree := NewSessionTree()
	tree.LoadMessages(sampleTreeMsgs(), "c")
	tree.Update(ToggleSessionTreeMsg{})
	out := strip(tree.View(80, 24))
	if !strings.Contains(out, "▸") {
		t.Fatalf("expected cursor glyph ▸, got %q", out)
	}
}
