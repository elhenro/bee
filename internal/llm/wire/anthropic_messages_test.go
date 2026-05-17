package wire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestBuildAnthropicMessages_System(t *testing.T) {
	req := BuildAnthropicMessagesRequest("claude-sonnet-4-6", "be brief", nil, nil, 0, 0, false, 0)
	if len(req.System) != 1 {
		t.Fatalf("want 1 system block, got %d", len(req.System))
	}
	if req.System[0].Text != "be brief" {
		t.Errorf("got %q", req.System[0].Text)
	}
	if req.MaxTokens == 0 {
		t.Error("MaxTokens should default to >0")
	}
}

func TestBuildAnthropicMessages_EmptySystemOmitted(t *testing.T) {
	req := BuildAnthropicMessagesRequest("claude-sonnet-4-6", "", nil, nil, 0, 0, false, 0)
	if len(req.System) != 0 {
		t.Fatalf("empty system should produce 0 blocks, got %d", len(req.System))
	}
}

func TestBuildAnthropicMessages_ToolRoundtrip(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "ls"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "calling tool"},
			{Type: types.BlockToolUse, Use: &types.ToolUse{
				ID: "u1", Name: "shell", Input: map[string]any{"cmd": "ls"},
			}},
		}},
		{Role: types.RoleTool, Content: []types.ContentBlock{{Type: types.BlockToolResult, Result: &types.ToolResult{
			UseID: "u1", Content: "file.go\n",
		}}}},
	}
	req := BuildAnthropicMessagesRequest("m", "", msgs, nil, 0, 0, true, 0)
	if len(req.Messages) != 3 {
		t.Fatalf("want 3 messages (user → assistant → user-tool), got %d: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != "user" || req.Messages[1].Role != "assistant" || req.Messages[2].Role != "user" {
		t.Errorf("role order wrong: %s/%s/%s", req.Messages[0].Role, req.Messages[1].Role, req.Messages[2].Role)
	}
	// assistant turn carries text + tool_use blocks (in that order)
	asst := req.Messages[1].Content
	if len(asst) != 2 || asst[0].Type != "text" || asst[1].Type != "tool_use" || asst[1].ID != "u1" || asst[1].Name != "shell" {
		t.Errorf("assistant blocks wrong: %+v", asst)
	}
	// tool result rides as a user-role tool_result block with nested text content
	tr := req.Messages[2].Content
	if len(tr) != 1 || tr[0].Type != "tool_result" || tr[0].ToolUseID != "u1" {
		t.Errorf("tool_result block wrong: %+v", tr)
	}
}

func TestBuildAnthropicMessages_StrictAlternation(t *testing.T) {
	// Two consecutive user messages should merge into one user message with
	// concatenated content (Anthropic rejects same-role neighbors).
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "a"}}},
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "b"}}},
		{Role: types.RoleAssistant, Content: []types.ContentBlock{{Type: types.BlockText, Text: "ok"}}},
	}
	req := BuildAnthropicMessagesRequest("m", "", msgs, nil, 0, 0, false, 0)
	if len(req.Messages) != 2 {
		t.Fatalf("want 2 merged messages, got %d: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != "user" || len(req.Messages[0].Content) != 2 {
		t.Errorf("first message should be user with 2 parts, got %+v", req.Messages[0])
	}
}

func TestBuildAnthropicMessages_ToolAdvert(t *testing.T) {
	tools := []ToolAdvert{{Name: "shell", Description: "run shell", Schema: map[string]any{"type": "object"}}}
	req := BuildAnthropicMessagesRequest("m", "", nil, tools, 0, 0, false, 0)
	if len(req.Tools) != 1 || req.Tools[0].Name != "shell" {
		t.Fatalf("tools missing/wrong: %+v", req.Tools)
	}
	if req.Tools[0].InputSchema["type"] != "object" {
		t.Errorf("input_schema not propagated: %+v", req.Tools[0].InputSchema)
	}
}

func TestBuildAnthropicMessages_ToolNamesPassThrough(t *testing.T) {
	// bee-native tool names must be sent verbatim. No vendor-specific remap.
	tools := []ToolAdvert{
		{Name: "shell", Schema: map[string]any{"type": "object"}},
		{Name: "read", Schema: map[string]any{"type": "object"}},
		{Name: "memory_search", Schema: map[string]any{"type": "object"}},
	}
	req := BuildAnthropicMessagesRequest("claude-sonnet-4-6", "", nil, tools, 0, 0, false, 0)
	if req.Tools[0].Name != "shell" || req.Tools[1].Name != "read" || req.Tools[2].Name != "memory_search" {
		t.Errorf("tool names should pass through unchanged, got %q / %q / %q",
			req.Tools[0].Name, req.Tools[1].Name, req.Tools[2].Name)
	}
}

func TestBuildAnthropicMessages_ThinkingBudget(t *testing.T) {
	req := BuildAnthropicMessagesRequest("m", "", nil, nil, 0, 0, false, 4096)
	if req.Thinking == nil || req.Thinking.BudgetTokens != 4096 {
		t.Fatalf("thinking budget not set: %+v", req.Thinking)
	}
	if req.Thinking.Type != "enabled" {
		t.Errorf("older models should use type=enabled, got %q", req.Thinking.Type)
	}
}

func TestBuildAnthropicMessages_AdaptiveThinking(t *testing.T) {
	// Sonnet 4.6 / Opus 4.6+ must use adaptive thinking (no budget_tokens).
	for _, m := range []string{"claude-sonnet-4-6", "claude-opus-4-7", "claude-opus-4-6"} {
		req := BuildAnthropicMessagesRequest(m, "", nil, nil, 0, 0, false, 4096)
		if req.Thinking == nil || req.Thinking.Type != "adaptive" {
			t.Errorf("%s: want adaptive thinking, got %+v", m, req.Thinking)
		}
		if req.Thinking != nil && req.Thinking.BudgetTokens != 0 {
			t.Errorf("%s: adaptive must NOT carry budget_tokens, got %d", m, req.Thinking.BudgetTokens)
		}
	}
	// Older Sonnet 4.5 stays on type=enabled.
	req := BuildAnthropicMessagesRequest("claude-sonnet-4-5", "", nil, nil, 0, 0, false, 4096)
	if req.Thinking.Type != "enabled" {
		t.Errorf("sonnet-4-5 should keep type=enabled, got %q", req.Thinking.Type)
	}
}

func TestBuildAnthropicMessages_TemperatureDroppedWhenThinking(t *testing.T) {
	req := BuildAnthropicMessagesRequest("claude-sonnet-4-6", "", nil, nil, 0, 0.7, false, 4096)
	if req.Temperature != nil {
		t.Errorf("temperature must be omitted when thinking is on, got %v", *req.Temperature)
	}
	// without thinking, temperature should pass through.
	req = BuildAnthropicMessagesRequest("claude-sonnet-4-6", "", nil, nil, 0, 0.7, false, 0)
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("temperature should pass when thinking is off, got %v", req.Temperature)
	}
}

func TestBuildAnthropicMessages_MaxTokensModelAware(t *testing.T) {
	cases := map[string]int{
		"claude-sonnet-4-6": 16384,
		"claude-sonnet-4-5": 16384,
		"claude-opus-4-7":   8192,
		"claude-haiku-4-5":  8192,
		"unknown-model":     4096,
	}
	for m, want := range cases {
		got := BuildAnthropicMessagesRequest(m, "", nil, nil, 0, 0, false, 0).MaxTokens
		if got != want {
			t.Errorf("%s max_tokens = %d, want %d", m, got, want)
		}
	}
	// explicit override always wins.
	got := BuildAnthropicMessagesRequest("claude-sonnet-4-6", "", nil, nil, 999, 0, false, 0).MaxTokens
	if got != 999 {
		t.Errorf("override ignored: %d", got)
	}
}

func TestNormalizeInputSchema(t *testing.T) {
	got := normalizeInputSchema(nil)
	if got["type"] != "object" {
		t.Errorf("nil schema → want type=object, got %+v", got)
	}
	got = normalizeInputSchema(map[string]any{"properties": map[string]any{"x": map[string]any{}}})
	if got["type"] != "object" {
		t.Errorf("missing type → want backfilled, got %+v", got)
	}
	// existing type preserved
	got = normalizeInputSchema(map[string]any{"type": "object", "properties": map[string]any{}})
	if got["type"] != "object" {
		t.Errorf("type preserved? got %+v", got)
	}
}

func TestParseAnthropicEvent_TextDelta(t *testing.T) {
	payload := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`)
	ev, err := ParseAnthropicEvent(payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev == nil || ev.Type != "content_block_delta" {
		t.Fatalf("got %+v", ev)
	}
	if ev.Delta == nil || ev.Delta.Text != "hello" {
		t.Errorf("text delta missing: %+v", ev.Delta)
	}
}

func TestParseAnthropicEvent_ToolUseStart(t *testing.T) {
	payload := []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_abc","name":"shell","input":{}}}`)
	ev, err := ParseAnthropicEvent(payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ContentBlock == nil || ev.ContentBlock.Name != "shell" || ev.ContentBlock.ID != "toolu_abc" {
		t.Fatalf("tool_use start wrong: %+v", ev.ContentBlock)
	}
}

func TestParseAnthropicEvent_MessageDeltaUsage(t *testing.T) {
	payload := []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":12,"output_tokens":34}}`)
	ev, err := ParseAnthropicEvent(payload)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Delta.StopReason != "end_turn" {
		t.Errorf("stop_reason missing: %+v", ev.Delta)
	}
	if ev.Usage == nil || ev.Usage.OutputTokens != 34 {
		t.Errorf("usage missing: %+v", ev.Usage)
	}
}

func TestBuildAnthropicMessages_JSONShape(t *testing.T) {
	// Smoke: full JSON round-trip parses cleanly and includes expected fields.
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}},
	}
	req := BuildAnthropicMessagesRequest("claude-haiku-4-5", "rules", msgs, nil, 1024, 0, true, 0)
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"model":"claude-haiku-4-5"`, `"max_tokens":1024`, `"stream":true`, `"system":[`, `"rules"`} {
		if !strings.Contains(s, want) {
			t.Errorf("payload missing %q: %s", want, s)
		}
	}
}
