package learn

import (
	"context"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func use(name string, in map[string]any) types.ContentBlock {
	return types.ContentBlock{Type: types.BlockToolUse, Use: &types.ToolUse{ID: name, Name: name, Input: in}}
}

func turn(blocks ...types.ContentBlock) []types.Message {
	return []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "go"}}},
		{Role: types.RoleAssistant, Content: blocks},
	}
}

func TestTouched(t *testing.T) {
	msgs := turn(
		use("edit", map[string]any{"path": "/w/a.go", "why": "fix nil"}),
		use("edit", map[string]any{"path": "a.go", "why": "fix nil"}),
		use("write", map[string]any{"path": "b.go"}),
		use("apply_patch", map[string]any{"patch": "--- a/c.go\n+++ b/c.go\n@@\n-x\n+y\n", "why": "rename"}),
		use("read", map[string]any{"path": "d.go"}),
	)
	got := Touched(msgs, "/w")
	if len(got) != 3 || got[0].Path != "a.go" || got[1].Path != "b.go" || got[2].Path != "c.go" {
		t.Fatalf("got %+v", got)
	}
	if len(got[0].Whys) != 1 || got[2].Whys[0] != "rename" {
		t.Fatalf("whys wrong: %+v", got)
	}
}

func userMsg(text string) types.Message {
	return types.Message{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: text}}}
}

func TestLastTurnStopsAtLastRequest(t *testing.T) {
	msgs := append(turn(use("write", map[string]any{"path": "old.go"})), userMsg("next"))
	if got := Touched(LastTurn(msgs), ""); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

// a steer or nudge the loop injected mid-turn is not a new request: edits made
// before it still belong to the turn's card
func TestChangeMapSpansInjectedUserTurns(t *testing.T) {
	msgs := append(turn(use("edit", map[string]any{"path": "a.go", "why": "A"})), userMsg("[steer] hurry up"),
		types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{use("edit", map[string]any{"path": "b.go", "why": "B"})}})
	s := ChangeMap(context.Background(), t.TempDir(), msgs)
	if !strings.Contains(s, "2 files") || !strings.Contains(s, "a.go") || !strings.Contains(s, "b.go") {
		t.Fatalf("got %q", s)
	}
	if w := Walkthrough(msgs, ""); strings.Contains(w, "2. ") {
		t.Fatalf("steer counted as a request: %q", w)
	}
}

func TestChangeMapEmptyWithoutMutations(t *testing.T) {
	if s := ChangeMap(context.Background(), t.TempDir(), turn(use("read", map[string]any{"path": "a"}))); s != "" {
		t.Fatalf("got %q", s)
	}
}

func TestChangeMapListsFilesAndReasons(t *testing.T) {
	s := ChangeMap(context.Background(), t.TempDir(), turn(use("edit", map[string]any{"path": "a.go", "why": "fix nil"})))
	if !strings.Contains(s, "1 file") || !strings.Contains(s, "a.go") || !strings.Contains(s, "fix nil") {
		t.Fatalf("got %q", s)
	}
}

func TestWalkthrough(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "fix the panic\nmore"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			use("edit", map[string]any{"path": "a.go", "why": "nil map on first call"}),
			{Type: types.BlockText, Text: "Guarded the map."},
		}},
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "add a test"}}},
	}
	got := Walkthrough(msgs, "")
	for _, want := range []string{"1. fix the panic", "a.go", "why: nil map on first call", "Guarded the map.", "2. add a test"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "more") {
		t.Errorf("only the first line of the ask belongs here:\n%s", got)
	}
}

func TestCardsAndQuiz(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "one"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{use("edit", map[string]any{"path": "a.go", "why": "A"})}},
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "two"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			use("edit", map[string]any{"path": "b.go", "why": "B"}),
			use("write", map[string]any{"path": "c.go"}),
		}},
	}
	cards := Cards(msgs, "", 5)
	if len(cards) != 2 || cards[0].Path != "b.go" || cards[1].Path != "a.go" {
		t.Fatalf("got %+v", cards)
	}
	if got := Cards(msgs, "", 1); len(got) != 1 {
		t.Fatalf("max ignored: %+v", got)
	}
	var out strings.Builder
	Quiz(strings.NewReader("my guess\n\n"), &out, cards)
	if !strings.Contains(out.String(), "agent: B") || !strings.Contains(out.String(), "agent: A") {
		t.Fatalf("got %q", out.String())
	}
}

func TestOutputStripsTerminalEscapes(t *testing.T) {
	evil := "\x1b]0;pwn\x07\x1b[31mred\x1b[0m\rx"
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: evil}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			use("edit", map[string]any{"path": "a\x1b[2J.go", "why": evil}),
			{Type: types.BlockText, Text: evil},
		}},
	}
	var q strings.Builder
	Quiz(strings.NewReader("\n"), &q, Cards(msgs, "", 3))
	for name, s := range map[string]string{
		"walkthrough": Walkthrough(msgs, ""),
		"changemap":   ChangeMap(context.Background(), t.TempDir(), msgs),
		"quiz":        q.String(),
	} {
		if strings.ContainsAny(s, "\x1b\x07\r") {
			t.Errorf("%s leaked control bytes: %q", name, s)
		}
	}
}

// a session that was opened and quit holds no messages: every aid says nothing
func TestEmptySessionIsQuiet(t *testing.T) {
	if w := Walkthrough(nil, ""); w != "" {
		t.Fatalf("walkthrough = %q", w)
	}
	if c := Cards(nil, "", 5); len(c) != 0 {
		t.Fatalf("cards = %+v", c)
	}
	if s := ChangeMap(context.Background(), t.TempDir(), nil); s != "" {
		t.Fatalf("change map = %q", s)
	}
	if LastTurn([]types.Message{}) != nil {
		t.Fatal("last turn of nothing should be nil")
	}
}
