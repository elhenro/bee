package loop

import (
	"strings"
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

func TestStripBrokenToolCall(t *testing.T) {
	asst := func(blocks ...types.ContentBlock) types.Message {
		return types.Message{ID: "a", Role: types.RoleAssistant, Content: blocks}
	}
	text := types.ContentBlock{Type: types.BlockText, Text: strings.Repeat("// long preamble that drove the wedge\n", 50)}
	tu := types.ContentBlock{Type: types.BlockToolUse, Use: &types.ToolUse{ID: "t1", Name: "write"}}

	t.Run("trailing message with tool_use dropped whole", func(t *testing.T) {
		// the provider-reject case: text preamble (file content) + broken
		// tool_use. aggressive strip drops BOTH, not just the tool_use.
		msgs := []types.Message{{ID: "u", Role: types.RoleUser}, asst(text, tu)}
		got := StripBrokenToolCall(msgs)
		if len(got) != 1 || got[0].ID != "u" {
			t.Fatalf("expected trailing assistant dropped whole, got %+v", got)
		}
	})

	t.Run("text-only trailing message untouched", func(t *testing.T) {
		// no tool_use → nothing to drop, return msgs as-is.
		msgs := []types.Message{asst(text)}
		got := StripBrokenToolCall(msgs)
		if len(got) != 1 || len(got[0].Content) != 1 {
			t.Fatalf("text-only message must be untouched, got %+v", got)
		}
	})

	t.Run("answered tool_use untouched", func(t *testing.T) {
		// tool_result already paired → last message is role=Tool, not the
		// assistant with tool_use. function is a no-op.
		msgs := []types.Message{
			asst(tu),
			{ID: "r", Role: types.RoleTool, Content: []types.ContentBlock{{Type: types.BlockToolResult}}},
		}
		got := StripBrokenToolCall(msgs)
		if len(got) != 2 {
			t.Fatalf("answered pair must be preserved, got %+v", got)
		}
	})

	t.Run("user message trailing is no-op", func(t *testing.T) {
		msgs := []types.Message{asst(tu), {ID: "u2", Role: types.RoleUser}}
		got := StripBrokenToolCall(msgs)
		if len(got) != 2 {
			t.Fatalf("trailing user message must be untouched, got %+v", got)
		}
	})

	t.Run("empty and nil safe", func(t *testing.T) {
		if got := StripBrokenToolCall(nil); got != nil {
			t.Fatalf("nil in, nil out: %+v", got)
		}
		if got := StripBrokenToolCall([]types.Message{}); len(got) != 0 {
			t.Fatalf("empty in, empty out: %+v", got)
		}
	})

	t.Run("input slice untouched", func(t *testing.T) {
		// defensive: must not mutate the caller's slice.
		msgs := []types.Message{{ID: "u", Role: types.RoleUser}, asst(text, tu)}
		_ = StripBrokenToolCall(msgs)
		if len(msgs) != 2 {
			t.Fatalf("input mutated: %+v", msgs)
		}
	})
}
