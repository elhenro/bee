package wire

import (
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func toolImageMsg() types.Message {
	return types.Message{Role: types.RoleTool, Content: []types.ContentBlock{
		{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "u1", Content: "image · a.png"}},
		{Type: types.BlockImage, MediaType: "image/png", Data: []byte("PNG")},
	}}
}

// a tool message carrying a picture keeps tool outputs adjacent to their
// calls and puts the picture after them
func TestChatToolMessageImageFollowsToolOutput(t *testing.T) {
	msgs := translateMessage(toolImageMsg())
	if len(msgs) != 2 || msgs[0].Role != "tool" || msgs[0].ToolCallID != "u1" || msgs[1].Role != "user" {
		t.Fatalf("want [tool, user], got %+v", msgs)
	}
	parts, ok := msgs[1].Content.([]map[string]any)
	if !ok || len(parts) != 1 || parts[0]["type"] != "image_url" {
		t.Fatalf("want one image_url part, got %#v", msgs[1].Content)
	}
}

func TestResponsesToolMessageImageFollowsToolOutput(t *testing.T) {
	items := translateResponsesMessage(toolImageMsg())
	if len(items) != 2 || items[0].Type != "function_call_output" || items[1].Role != "user" {
		t.Fatalf("want [function_call_output, user message], got %+v", items)
	}
}

func TestAnthropicToolMessageImageAfterToolResult(t *testing.T) {
	m, ok := translateAnthropicMessage(toolImageMsg())
	if !ok || len(m.Content) != 2 || m.Content[0].Type != "tool_result" || m.Content[1].Type != "image" {
		t.Fatalf("want [tool_result, image], got %+v", m.Content)
	}
}
