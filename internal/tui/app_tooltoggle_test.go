package tui

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestLastToolResultMessage(t *testing.T) {
	tr := types.Message{Role: types.RoleTool, Content: []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{Content: "out"}},
	}}
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}},
		tr,
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "done"}}},
	}
	got, ok := lastToolResultMessage(msgs)
	if !ok {
		t.Fatal("expected a tool result message")
	}
	if got.Content[0].Result.Content != "out" {
		t.Fatalf("wrong message returned: %+v", got)
	}
}

func TestLastToolResultMessageNone(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}},
	}
	if _, ok := lastToolResultMessage(msgs); ok {
		t.Fatal("expected no tool result")
	}
}

// full render must surface more rows than the collapsed preview for a long
// tool result — the core of the ctrl+o toggle.
func TestRenderMessageDetailCapping(t *testing.T) {
	long := ""
	for i := 0; i < 40; i++ {
		long += "line\n"
	}
	msg := types.Message{Role: types.RoleTool, Content: []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{Content: long}},
	}}
	r := NewStreamRenderer(DefaultStyles(), 80)
	collapsed := r.RenderMessageDetail(msg, false)
	full := r.RenderMessageDetail(msg, true)
	if countLines(full) <= countLines(collapsed) {
		t.Fatalf("full (%d) should exceed collapsed (%d)", countLines(full), countLines(collapsed))
	}
	// toggling back must not leave verbose stuck on.
	if r.verbose {
		t.Fatal("verbose leaked after RenderMessageDetail")
	}
}

func countLines(s string) int {
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
