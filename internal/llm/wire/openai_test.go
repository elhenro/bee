package wire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestBuildRequest_TextOnly(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "hello"},
		}},
	}
	req := BuildRequest("gpt-4o-mini", "you are bee", msgs, nil, 100, 0.5, true)

	if req.Model != "gpt-4o-mini" {
		t.Fatalf("model: got %q", req.Model)
	}
	if !req.Stream {
		t.Fatal("stream not set")
	}
	if req.Temperature == nil || *req.Temperature != 0.5 {
		t.Fatalf("temperature: got %v", req.Temperature)
	}
	if req.MaxTokens != 100 {
		t.Fatalf("max tokens: got %d", req.MaxTokens)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages: got %d, want 2", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "you are bee" {
		t.Fatalf("system message wrong: %+v", req.Messages[0])
	}
	if req.Messages[1].Role != "user" || req.Messages[1].Content != "hello" {
		t.Fatalf("user message wrong: %+v", req.Messages[1])
	}

	// round-trip via JSON, ensure clean encoding
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"role":"user"`) {
		t.Fatalf("missing user role in JSON: %s", b)
	}
}

func TestBuildRequest_TemperatureZeroOmitted(t *testing.T) {
	req := BuildRequest("m", "", nil, nil, 0, 0, false)
	if req.Temperature != nil {
		t.Fatalf("temperature should be omitted when zero, got %v", *req.Temperature)
	}
	b, _ := json.Marshal(req)
	if strings.Contains(string(b), "temperature") {
		t.Fatalf("temperature key leaked into JSON: %s", b)
	}
}

func TestBuildRequest_ToolUseTranslation(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "running shell"},
			{Type: types.BlockToolUse, Use: &types.ToolUse{
				ID:    "call_42",
				Name:  "shell",
				Input: map[string]any{"cmd": "ls"},
			}},
		}},
	}
	req := BuildRequest("m", "", msgs, nil, 0, 0, false)
	if len(req.Messages) != 1 {
		t.Fatalf("messages: got %d, want 1", len(req.Messages))
	}
	a := req.Messages[0]
	if a.Role != "assistant" {
		t.Fatalf("role: got %q", a.Role)
	}
	if a.Content != "running shell" {
		t.Fatalf("content: got %q", a.Content)
	}
	if len(a.ToolCalls) != 1 {
		t.Fatalf("tool calls: got %d", len(a.ToolCalls))
	}
	tc := a.ToolCalls[0]
	if tc.ID != "call_42" || tc.Type != "function" || tc.Function.Name != "shell" {
		t.Fatalf("tool call shape wrong: %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("args parse: %v", err)
	}
	if args["cmd"] != "ls" {
		t.Fatalf("args: got %v", args)
	}
}

func TestBuildRequest_ToolResultTranslation(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "also fyi"},
			{Type: types.BlockToolResult, Result: &types.ToolResult{
				UseID:   "call_42",
				Content: "total 0",
			}},
			{Type: types.BlockToolResult, Result: &types.ToolResult{
				UseID:   "call_99",
				Content: "boom",
				IsError: true,
			}},
		}},
	}
	req := BuildRequest("m", "", msgs, nil, 0, 0, false)
	if len(req.Messages) != 3 {
		t.Fatalf("messages: got %d, want 3 (user text + 2 tool)", len(req.Messages))
	}
	if req.Messages[0].Role != "user" || req.Messages[0].Content != "also fyi" {
		t.Fatalf("first msg wrong: %+v", req.Messages[0])
	}
	tool1 := req.Messages[1]
	if tool1.Role != "tool" || tool1.ToolCallID != "call_42" || tool1.Content != "total 0" {
		t.Fatalf("tool1 wrong: %+v", tool1)
	}
	tool2 := req.Messages[2]
	tool2Content, _ := tool2.Content.(string)
	if tool2.Role != "tool" || tool2.ToolCallID != "call_99" || !strings.HasPrefix(tool2Content, "ERROR:") {
		t.Fatalf("tool2 wrong: %+v", tool2)
	}
}

func TestBuildRequest_ImageBlockEmitsImageURLParts(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{
			{Type: types.BlockText, Text: "what is this?"},
			{Type: types.BlockImage, MediaType: "image/png", Data: []byte{0x89, 0x50, 0x4e, 0x47}},
		}},
	}
	req := BuildRequest("gpt-4o", "", msgs, nil, 0, 0, false)
	if len(req.Messages) != 1 {
		t.Fatalf("messages: got %d, want 1", len(req.Messages))
	}
	parts, ok := req.Messages[0].Content.([]map[string]any)
	if !ok {
		t.Fatalf("expected typed-parts array, got %T: %+v", req.Messages[0].Content, req.Messages[0].Content)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image_url), got %d: %+v", len(parts), parts)
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "what is this?" {
		t.Errorf("text part: %+v", parts[0])
	}
	if parts[1]["type"] != "image_url" {
		t.Errorf("image part type: %+v", parts[1])
	}
	img, _ := parts[1]["image_url"].(map[string]any)
	url, _ := img["url"].(string)
	if !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Errorf("bad data url prefix: %q", url)
	}
	// round-trip via JSON so the wire shape is provably stable.
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"image_url"`) {
		t.Errorf("missing image_url in JSON: %s", b)
	}
}

func TestBuildRequest_TextOnlyKeepsStringContent(t *testing.T) {
	// regression: text-only user message should still serialize content as a
	// plain string so non-vision providers don't choke on the parts schema.
	msgs := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}},
	}
	req := BuildRequest("m", "", msgs, nil, 0, 0, false)
	if s, ok := req.Messages[0].Content.(string); !ok || s != "hi" {
		t.Fatalf("expected string content, got %T %v", req.Messages[0].Content, req.Messages[0].Content)
	}
}

func TestParseChunk_TextDelta(t *testing.T) {
	raw := []byte(`{"id":"x","choices":[{"index":0,"delta":{"content":"hi"}}]}`)
	c, done, err := ParseChunk(raw)
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatal("done unexpectedly true")
	}
	if c.Choices[0].Delta.Content != "hi" {
		t.Fatalf("content: got %q", c.Choices[0].Delta.Content)
	}
}

func TestParseChunk_DoneMarker(t *testing.T) {
	c, done, err := ParseChunk([]byte("[DONE]"))
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("done should be true for [DONE] marker")
	}
	if c != nil {
		t.Fatal("chunk should be nil on [DONE]")
	}

	// also handle padded form
	_, done, err = ParseChunk([]byte("  [DONE]  "))
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("padded [DONE] should be detected")
	}
}

func TestParseChunk_Empty(t *testing.T) {
	c, done, err := ParseChunk([]byte(""))
	if err != nil {
		t.Fatal(err)
	}
	if done || c != nil {
		t.Fatal("empty chunk should be no-op")
	}
}

func TestToolCallAccumulator_MultiChunk(t *testing.T) {
	acc := NewToolCallAccumulator()
	acc.Apply([]StreamToolCall{{Index: 0, ID: "call_1", Function: StreamFunctionDelta{Name: "shell"}}})
	acc.Apply([]StreamToolCall{{Index: 0, Function: StreamFunctionDelta{Arguments: `{"cmd":"l`}}})
	acc.Apply([]StreamToolCall{{Index: 0, Function: StreamFunctionDelta{Arguments: `s"}`}}})

	calls, err := acc.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls: got %d", len(calls))
	}
	if calls[0].ID != "call_1" || calls[0].Name != "shell" {
		t.Fatalf("call: %+v", calls[0])
	}
	if calls[0].Input["cmd"] != "ls" {
		t.Fatalf("input: %+v", calls[0].Input)
	}
}

func TestToolCallAccumulator_EmptyArgs(t *testing.T) {
	acc := NewToolCallAccumulator()
	acc.Apply([]StreamToolCall{{Index: 0, ID: "c", Function: StreamFunctionDelta{Name: "ping"}}})
	calls, err := acc.Finalize()
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].Name != "ping" {
		t.Fatalf("calls: %+v", calls)
	}
	if len(calls[0].Input) != 0 {
		t.Fatalf("input should be empty map, got %+v", calls[0].Input)
	}
}

func TestToolCallAccumulator_RepairsTrailingBrace(t *testing.T) {
	// v4-flash sometimes emits an extra `}` past a balanced object.
	acc := NewToolCallAccumulator()
	acc.Apply([]StreamToolCall{{Index: 0, ID: "c", Function: StreamFunctionDelta{Name: "read"}}})
	acc.Apply([]StreamToolCall{{Index: 0, Function: StreamFunctionDelta{Arguments: `{"path":"a","limit":50}}`}}})
	calls, err := acc.Finalize()
	if err != nil {
		t.Fatalf("expected repair to succeed, got %v", err)
	}
	if calls[0].Input["path"] != "a" {
		t.Fatalf("repaired input wrong: %+v", calls[0].Input)
	}
}

func TestToolCallAccumulator_RepairsUnterminatedObject(t *testing.T) {
	// model truncated mid-call.
	acc := NewToolCallAccumulator()
	acc.Apply([]StreamToolCall{{Index: 0, ID: "c", Function: StreamFunctionDelta{Name: "shell"}}})
	acc.Apply([]StreamToolCall{{Index: 0, Function: StreamFunctionDelta{Arguments: `{"cmd":"ls"`}}})
	calls, err := acc.Finalize()
	if err != nil {
		t.Fatalf("expected repair to close unterminated object, got %v", err)
	}
	if calls[0].Input["cmd"] != "ls" {
		t.Fatalf("repaired input wrong: %+v", calls[0].Input)
	}
}

func TestToolCallAccumulator_RepairsLeadingProse(t *testing.T) {
	acc := NewToolCallAccumulator()
	acc.Apply([]StreamToolCall{{Index: 0, ID: "c", Function: StreamFunctionDelta{Name: "shell"}}})
	acc.Apply([]StreamToolCall{{Index: 0, Function: StreamFunctionDelta{Arguments: `here you go: {"cmd":"pwd"}`}}})
	calls, err := acc.Finalize()
	if err != nil {
		t.Fatalf("expected repair to strip leading prose, got %v", err)
	}
	if calls[0].Input["cmd"] != "pwd" {
		t.Fatalf("repaired input wrong: %+v", calls[0].Input)
	}
}

func TestToolCallAccumulator_RepairsUnterminatedString(t *testing.T) {
	acc := NewToolCallAccumulator()
	acc.Apply([]StreamToolCall{{Index: 0, ID: "c", Function: StreamFunctionDelta{Name: "shell"}}})
	acc.Apply([]StreamToolCall{{Index: 0, Function: StreamFunctionDelta{Arguments: `{"cmd":"ls -la`}}})
	calls, err := acc.Finalize()
	if err != nil {
		t.Fatalf("expected repair to close unterminated string, got %v", err)
	}
	if calls[0].Input["cmd"] != "ls -la" {
		t.Fatalf("repaired input wrong: %+v", calls[0].Input)
	}
}

func TestToolCallAccumulator_BadInputSurfacesParseError(t *testing.T) {
	// no braces at all — repair gives up. caller must get a structured
	// signal (RawArgs + ParseError) instead of swallowing into empty args.
	acc := NewToolCallAccumulator()
	acc.Apply([]StreamToolCall{{Index: 0, ID: "c", Function: StreamFunctionDelta{Name: "shell"}}})
	acc.Apply([]StreamToolCall{{Index: 0, Function: StreamFunctionDelta{Arguments: `not json at all`}}})
	calls, err := acc.Finalize()
	if err != nil {
		t.Fatalf("Finalize should no longer error; got %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls: got %d", len(calls))
	}
	if calls[0].ParseError == "" {
		t.Fatalf("expected ParseError, got empty: %+v", calls[0])
	}
	if calls[0].RawArgs != "not json at all" {
		t.Fatalf("RawArgs: got %q", calls[0].RawArgs)
	}
	if len(calls[0].Input) != 0 {
		t.Fatalf("Input should be empty on parse failure, got %+v", calls[0].Input)
	}
}

func TestToolCallAccumulator_TrailingCommaRepaired(t *testing.T) {
	// {"path":"x",} — trailing comma. wire repair handles this case via
	// stripTrailingCommas in repairToolArgs (case: walks to closing brace,
	// the leftover comma is benign), so we get clean input back.
	acc := NewToolCallAccumulator()
	acc.Apply([]StreamToolCall{{Index: 0, ID: "c", Function: StreamFunctionDelta{Name: "read"}}})
	acc.Apply([]StreamToolCall{{Index: 0, Function: StreamFunctionDelta{Arguments: `{"path":"x",}`}}})
	calls, err := acc.Finalize()
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls: got %d", len(calls))
	}
	// either the repair succeeded (path=x) or it surfaces ParseError.
	// pick the surface-error contract: assert we never silently lose data.
	if calls[0].ParseError != "" {
		if calls[0].RawArgs != `{"path":"x",}` {
			t.Fatalf("RawArgs missing on parse failure: %+v", calls[0])
		}
	} else if calls[0].Input["path"] != "x" {
		t.Fatalf("repair lost path: %+v", calls[0].Input)
	}
}

func TestFormatToolArgs(t *testing.T) {
	out, err := FormatToolArgs(nil)
	if err != nil || out != "{}" {
		t.Fatalf("nil case: out=%q err=%v", out, err)
	}
	out, err = FormatToolArgs(map[string]any{"k": "v"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"k":"v"`) {
		t.Fatalf("got %q", out)
	}
}
