package bench

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestMetricsFromMessages(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "go"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockToolUse, Use: &types.ToolUse{Name: "read"}},
		}},
		{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{}},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockToolUse, Use: &types.ToolUse{Name: "nope"}},
		}},
		{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{IsError: true}},
		}},
	}
	m := MetricsFromMessages(msgs, true)

	if m.Turns != 2 {
		t.Errorf("turns = %d, want 2", m.Turns)
	}
	if m.ToolCalls != 2 {
		t.Errorf("tool calls = %d, want 2", m.ToolCalls)
	}
	if m.ErroredCalls != 1 {
		t.Errorf("errored = %d, want 1", m.ErroredCalls)
	}
	if !m.StoppedClean {
		t.Error("stoppedClean should propagate")
	}
}
