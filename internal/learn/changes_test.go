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

func TestTouchedStopsAtLastUser(t *testing.T) {
	msgs := append(turn(use("write", map[string]any{"path": "old.go"})),
		types.Message{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "next"}}})
	if got := Touched(msgs, ""); len(got) != 0 {
		t.Fatalf("got %+v", got)
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
