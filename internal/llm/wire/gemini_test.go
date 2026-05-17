package wire

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGeminiRequest_RoundTrip(t *testing.T) {
	req := GeminiRequest{
		SystemInstruction: &GeminiContent{
			Parts: []GeminiPart{{Text: "you are bee"}},
		},
		Contents: []GeminiContent{
			{
				Role: "user",
				Parts: []GeminiPart{
					{Text: "hi"},
					{InlineData: &GeminiInlineData{MimeType: "image/png", Data: "AAAA"}},
				},
			},
			{
				Role: "model",
				Parts: []GeminiPart{
					{FunctionCall: &GeminiFunctionCall{Name: "shell", Args: map[string]any{"cmd": "ls"}}},
				},
			},
			{
				Role: "user",
				Parts: []GeminiPart{
					{FunctionResponse: &GeminiFunctionResponse{
						Name:     "shell",
						Response: map[string]any{"content": "total 0"},
					}},
				},
			},
		},
		Tools: []GeminiTool{{
			FunctionDeclarations: []GeminiFunctionDecl{{
				Name:        "shell",
				Description: "run shell command",
				Parameters:  map[string]any{"type": "object"},
			}},
		}},
		GenerationConfig: map[string]any{
			"thinkingConfig": map[string]any{"thinkingBudget": 1024},
		},
	}

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	// key wire-shape invariants
	for _, want := range []string{
		`"systemInstruction"`,
		`"role":"user"`,
		`"role":"model"`,
		`"inline_data"`,
		`"functionCall"`,
		`"functionResponse"`,
		`"function_declarations"`,
		`"thinkingBudget"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in JSON: %s", want, s)
		}
	}

	var back GeminiRequest
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.SystemInstruction == nil || back.SystemInstruction.Parts[0].Text != "you are bee" {
		t.Errorf("system round-trip lost text: %+v", back.SystemInstruction)
	}
	if len(back.Contents) != 3 {
		t.Fatalf("contents lost: got %d", len(back.Contents))
	}
	if back.Contents[0].Parts[1].InlineData.Data != "AAAA" {
		t.Errorf("inline_data lost")
	}
	if back.Contents[1].Parts[0].FunctionCall.Args["cmd"] != "ls" {
		t.Errorf("functionCall args lost")
	}
}

func TestGeminiStreamChunk_Decode(t *testing.T) {
	raw := `{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`
	var c GeminiStreamChunk
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Candidates) != 1 {
		t.Fatalf("candidates: got %d", len(c.Candidates))
	}
	if c.Candidates[0].Content.Parts[0].Text != "hello" {
		t.Errorf("text mismatch")
	}
	if c.Candidates[0].FinishReason != "STOP" {
		t.Errorf("finishReason: got %q", c.Candidates[0].FinishReason)
	}
	if c.UsageMetadata == nil || c.UsageMetadata.PromptTokenCount != 5 {
		t.Errorf("usage missing: %+v", c.UsageMetadata)
	}
}

func TestGeminiStreamChunk_FunctionCallShape(t *testing.T) {
	raw := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"shell","args":{"cmd":"ls"}}}],"role":"model"}}]}`
	var c GeminiStreamChunk
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	fc := c.Candidates[0].Content.Parts[0].FunctionCall
	if fc == nil || fc.Name != "shell" || fc.Args["cmd"] != "ls" {
		t.Errorf("functionCall decoded wrong: %+v", fc)
	}
}
