package wire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestAnthropicThinkingBudgetClamped(t *testing.T) {
	cases := []struct {
		name      string
		maxTokens int
		budget    int
		want      int // 0 = thinking off
	}{
		{"under max", 16384, 4096, 4096},
		{"equal to max", 4096, 4096, 3072},
		{"above max", 8192, 16384, 6144},
		{"max too small", 1000, 4096, 0},
		{"clamp falls under floor", 1200, 4096, 0},
	}
	for _, c := range cases {
		req := BuildAnthropicMessagesRequest("claude-sonnet-4-5", "", "", nil, nil, c.maxTokens, 0.5, false, c.budget)
		if c.want == 0 {
			if req.Thinking != nil {
				t.Errorf("%s: want thinking off, got %+v", c.name, req.Thinking)
			}
			if req.Temperature == nil {
				t.Errorf("%s: temperature should pass through when thinking is off", c.name)
			}
			continue
		}
		if req.Thinking == nil || req.Thinking.BudgetTokens != c.want {
			t.Errorf("%s: want budget %d, got %+v", c.name, c.want, req.Thinking)
		}
		if req.Thinking != nil && req.Thinking.BudgetTokens >= req.MaxTokens {
			t.Errorf("%s: budget %d must stay below max_tokens %d", c.name, req.Thinking.BudgetTokens, req.MaxTokens)
		}
	}
}

func TestAnthropicEmptyToolResultGetsPlaceholder(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockToolUse, Use: &types.ToolUse{ID: "t1", Name: "bash", Input: map[string]any{}}},
		}},
		{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "t1", Content: ""}},
		}},
	}
	req := BuildAnthropicMessagesRequest("claude-sonnet-4-5", "", "", msgs, nil, 0, 0, false, 0)
	raw, _ := json.Marshal(req)
	if !strings.Contains(string(raw), `"text":"(no output)"`) {
		t.Errorf("empty tool result must carry a text placeholder: %s", raw)
	}
	if !strings.Contains(string(raw), `"input":{}`) {
		t.Errorf("tool_use must keep an empty input object: %s", raw)
	}
}

func TestAnthropicThinkingReplay(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "ok"},
			{Type: types.BlockThinking, Text: "hmm", Signature: "sig-1"},
			{Type: types.BlockToolUse, Use: &types.ToolUse{ID: "t1", Name: "bash", Input: map[string]any{"c": "ls"}}},
		}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockThinking, Text: "unsigned"},
			{Type: types.BlockText, Text: "hi"},
		}},
	}
	// two assistant turns merge; check the first message directly
	am, ok := translateAnthropicMessage(msgs[0])
	if !ok || len(am.Content) != 3 {
		t.Fatalf("want 3 parts, got %+v", am)
	}
	first := am.Content[0]
	if first.Type != "thinking" || first.Signature != "sig-1" || first.Thinking == nil || *first.Thinking != "hmm" {
		t.Errorf("signed thinking must lead the turn, got %+v", first)
	}
	am2, _ := translateAnthropicMessage(msgs[1])
	if len(am2.Content) != 1 || am2.Content[0].Type != "text" {
		t.Errorf("unsigned thinking must be dropped, got %+v", am2.Content)
	}
}

func TestAnthropicEmptySignedThinkingKeepsField(t *testing.T) {
	am, _ := translateAnthropicMessage(types.Message{Role: types.RoleAssistant, Content: []types.ContentBlock{
		{Type: types.BlockThinking, Signature: "s"},
		{Type: types.BlockText, Text: "x"},
	}})
	raw, _ := json.Marshal(am)
	if !strings.Contains(string(raw), `"thinking":""`) {
		t.Errorf("thinking field must be present even when empty: %s", raw)
	}
}
