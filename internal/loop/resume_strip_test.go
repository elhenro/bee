package loop

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestStripDanglingToolUse(t *testing.T) {
	asst := func(blocks ...types.ContentBlock) types.Message {
		return types.Message{ID: "a", Role: types.RoleAssistant, Content: blocks}
	}
	text := types.ContentBlock{Type: types.BlockText, Text: "working on it"}
	tu := types.ContentBlock{Type: types.BlockToolUse, Use: &types.ToolUse{ID: "t1", Name: "shell"}}

	t.Run("trailing tool_use only message dropped", func(t *testing.T) {
		msgs := []types.Message{{ID: "u", Role: types.RoleUser}, asst(tu)}
		got := StripDanglingToolUse(msgs)
		if len(got) != 1 || got[0].ID != "u" {
			t.Fatalf("expected dangling assistant dropped, got %+v", got)
		}
	})

	t.Run("mixed message keeps text", func(t *testing.T) {
		msgs := []types.Message{asst(text, tu)}
		got := StripDanglingToolUse(msgs)
		if len(got) != 1 || len(got[0].Content) != 1 || got[0].Content[0].Type != types.BlockText {
			t.Fatalf("expected text kept tool_use stripped, got %+v", got)
		}
		// input slice untouched
		if len(msgs[0].Content) != 2 {
			t.Fatal("input mutated")
		}
	})

	t.Run("answered tool_use untouched", func(t *testing.T) {
		msgs := []types.Message{
			asst(tu),
			{ID: "r", Role: types.RoleTool, Content: []types.ContentBlock{{Type: types.BlockToolResult}}},
		}
		got := StripDanglingToolUse(msgs)
		if len(got) != 2 {
			t.Fatalf("answered pair must be preserved, got %+v", got)
		}
	})

	t.Run("empty and nil safe", func(t *testing.T) {
		if got := StripDanglingToolUse(nil); got != nil {
			t.Fatalf("nil in, nil out: %+v", got)
		}
	})
}
