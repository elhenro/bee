package wire

import (
	"encoding/json"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestBuildResponsesRequest_SystemBecomesInstructions(t *testing.T) {
	req := BuildResponsesRequest("gpt-5-codex", "be brief", nil, nil, 0, 0, false, "")
	if req.Instructions != "be brief" {
		t.Errorf("instructions = %q", req.Instructions)
	}
	if len(req.Input) != 0 {
		t.Errorf("system should not appear in input, got %d items", len(req.Input))
	}
}

func TestBuildResponsesRequest_UserMessage(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hello"}}},
	}
	req := BuildResponsesRequest("m", "", msgs, nil, 0, 0, true, "")
	if !req.Stream {
		t.Error("stream not set")
	}
	if len(req.Input) != 1 || req.Input[0].Type != "message" || req.Input[0].Role != "user" {
		t.Fatalf("bad input: %+v", req.Input)
	}
	if len(req.Input[0].Content) != 1 || req.Input[0].Content[0].Type != "input_text" || req.Input[0].Content[0].Text != "hello" {
		t.Errorf("bad content: %+v", req.Input[0].Content)
	}
}

func TestBuildResponsesRequest_ToolCallRoundtrip(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleAssistant,
			Content: []types.ContentBlock{
				{Type: types.BlockText, Text: "running shell"},
				{Type: types.BlockToolUse, Use: &types.ToolUse{ID: "call_1", Name: "shell", Input: map[string]any{"cmd": "ls"}}},
			},
		},
		{
			Role: types.RoleUser,
			Content: []types.ContentBlock{
				{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "call_1", Content: "a\nb\n"}},
			},
		},
	}
	req := BuildResponsesRequest("m", "", msgs, nil, 0, 0, false, "")
	if len(req.Input) != 3 {
		t.Fatalf("want 3 items (message+function_call+function_call_output), got %d: %+v", len(req.Input), req.Input)
	}
	if req.Input[0].Type != "message" || req.Input[0].Role != "assistant" {
		t.Errorf("item 0 = %+v", req.Input[0])
	}
	if req.Input[1].Type != "function_call" || req.Input[1].CallID != "call_1" || req.Input[1].Name != "shell" {
		t.Errorf("item 1 = %+v", req.Input[1])
	}
	if req.Input[2].Type != "function_call_output" || req.Input[2].CallID != "call_1" || req.Input[2].Output != "a\nb\n" {
		t.Errorf("item 2 = %+v", req.Input[2])
	}
}

func TestBuildResponsesRequest_ToolAdvert(t *testing.T) {
	tools := []ToolAdvert{
		{Name: "shell", Description: "run cmd", Schema: map[string]any{"type": "object"}},
	}
	req := BuildResponsesRequest("m", "", nil, tools, 0, 0, false, "high")
	if len(req.Tools) != 1 {
		t.Fatalf("got %d tools", len(req.Tools))
	}
	if req.Tools[0].Type != "function" || req.Tools[0].Name != "shell" {
		t.Errorf("tool 0 = %+v", req.Tools[0])
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "high" {
		t.Errorf("reasoning = %+v", req.Reasoning)
	}
}

func TestBuildResponsesRequest_EmptyToolResultStillSerializes(t *testing.T) {
	msgs := []types.Message{
		{
			Role: types.RoleUser,
			Content: []types.ContentBlock{
				{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "call_x", Content: ""}},
			},
		},
	}
	req := BuildResponsesRequest("m", "", msgs, nil, 0, 0, false, "")
	if len(req.Input) != 1 || req.Input[0].Type != "function_call_output" {
		t.Fatalf("bad input: %+v", req.Input)
	}
	if req.Input[0].Output == "" {
		t.Fatalf("output must not be empty: %+v", req.Input[0])
	}
	body, _ := json.Marshal(req)
	if !contains(string(body), `"output":`) {
		t.Errorf("serialized body missing output key: %s", body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestParseResponsesEvent_TextDelta(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"type":  "response.output_text.delta",
		"delta": "hi",
	})
	ev, err := ParseResponsesEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "response.output_text.delta" || ev.Delta != "hi" {
		t.Errorf("got %+v", ev)
	}
}

func TestParseResponsesEvent_OutputItemFunctionCall(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"type": "response.output_item.added",
		"item": map[string]any{
			"id":      "item_1",
			"type":    "function_call",
			"call_id": "call_abc",
			"name":    "shell",
		},
	})
	ev, err := ParseResponsesEvent(raw)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Item == nil || ev.Item.Type != "function_call" || ev.Item.CallID != "call_abc" {
		t.Errorf("got %+v", ev.Item)
	}
}

func TestParseResponsesEvent_Empty(t *testing.T) {
	ev, err := ParseResponsesEvent([]byte("  "))
	if err != nil {
		t.Fatal(err)
	}
	if ev != nil {
		t.Errorf("expected nil, got %+v", ev)
	}
}
