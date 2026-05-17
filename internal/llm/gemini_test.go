package llm

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestBuildGeminiRequest_Basic(t *testing.T) {
	req := Request{
		System: "you are bee",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}},
		},
	}
	g := buildGeminiRequest(req)
	if g.SystemInstruction == nil || g.SystemInstruction.Parts[0].Text != "you are bee" {
		t.Errorf("system mismatch: %+v", g.SystemInstruction)
	}
	if len(g.Contents) != 1 || g.Contents[0].Role != "user" {
		t.Errorf("contents wrong: %+v", g.Contents)
	}
	if g.Contents[0].Parts[0].Text != "hi" {
		t.Errorf("text part wrong: %+v", g.Contents[0].Parts[0])
	}
}

func TestBuildGeminiRequest_Image(t *testing.T) {
	req := Request{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{
				{Type: types.BlockImage, MediaType: "image/png", Data: []byte{0xFF, 0xD8}},
			}},
		},
	}
	g := buildGeminiRequest(req)
	if g.Contents[0].Parts[0].InlineData == nil {
		t.Fatal("expected inline_data")
	}
	want := base64.StdEncoding.EncodeToString([]byte{0xFF, 0xD8})
	if g.Contents[0].Parts[0].InlineData.Data != want {
		t.Errorf("base64 mismatch: got %q want %q", g.Contents[0].Parts[0].InlineData.Data, want)
	}
	if g.Contents[0].Parts[0].InlineData.MimeType != "image/png" {
		t.Errorf("mime mismatch: %q", g.Contents[0].Parts[0].InlineData.MimeType)
	}
}

func TestBuildGeminiRequest_ImageDefaultMime(t *testing.T) {
	req := Request{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{
				{Type: types.BlockImage, Data: []byte{0x00}},
			}},
		},
	}
	g := buildGeminiRequest(req)
	if g.Contents[0].Parts[0].InlineData.MimeType != "image/png" {
		t.Errorf("default mime should be image/png, got %q", g.Contents[0].Parts[0].InlineData.MimeType)
	}
}

func TestBuildGeminiRequest_Tools(t *testing.T) {
	req := Request{
		Tools: []ToolSpec{{Name: "echo", Description: "echo input", Schema: map[string]any{"type": "object"}}},
	}
	g := buildGeminiRequest(req)
	if len(g.Tools) != 1 || len(g.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools missing: %+v", g.Tools)
	}
	d := g.Tools[0].FunctionDeclarations[0]
	if d.Name != "echo" || d.Description != "echo input" {
		t.Errorf("decl mismatch: %+v", d)
	}
	if d.Parameters["type"] != "object" {
		t.Errorf("parameters lost: %+v", d.Parameters)
	}
}

func TestBuildGeminiRequest_ToolUseRoundTrip(t *testing.T) {
	req := Request{
		Messages: []types.Message{
			{Role: types.RoleAssistant, Content: []types.ContentBlock{
				{Type: types.BlockText, Text: "running shell"},
				{Type: types.BlockToolUse, Use: &types.ToolUse{
					ID: "call_42", Name: "shell", Input: map[string]any{"cmd": "ls"},
				}},
			}},
			{Role: types.RoleUser, Content: []types.ContentBlock{
				{Type: types.BlockToolResult, Result: &types.ToolResult{
					UseID: "call_42", Content: "total 0",
				}},
			}},
		},
	}
	g := buildGeminiRequest(req)
	if len(g.Contents) != 2 {
		t.Fatalf("expected 2 contents, got %d: %+v", len(g.Contents), g.Contents)
	}
	if g.Contents[0].Role != "model" {
		t.Errorf("assistant role: got %q", g.Contents[0].Role)
	}
	fc := g.Contents[0].Parts[1].FunctionCall
	if fc == nil || fc.Name != "shell" || fc.Args["cmd"] != "ls" {
		t.Errorf("functionCall wrong: %+v", fc)
	}
	fr := g.Contents[1].Parts[0].FunctionResponse
	if fr == nil || fr.Response["content"] != "total 0" {
		t.Errorf("functionResponse wrong: %+v", fr)
	}
}

func TestBuildGeminiRequest_Thinking(t *testing.T) {
	req := Request{Thinking: ThinkingHigh}
	g := buildGeminiRequest(req)
	if g.GenerationConfig == nil {
		t.Fatal("expected generationConfig")
	}
	tc, ok := g.GenerationConfig["thinkingConfig"].(map[string]any)
	if !ok || tc["thinkingBudget"] != 16384 {
		t.Errorf("thinkingConfig wrong: %+v", g.GenerationConfig)
	}
}

func TestBuildGeminiRequest_ThinkingOffOmits(t *testing.T) {
	req := Request{Thinking: ThinkingOff}
	g := buildGeminiRequest(req)
	if g.GenerationConfig != nil {
		if _, has := g.GenerationConfig["thinkingConfig"]; has {
			t.Errorf("thinkingConfig should be omitted when off")
		}
	}
}

func TestBuildGeminiRequest_EmptyMessages(t *testing.T) {
	// regression: empty content blocks must not produce a Content with
	// zero parts, which gemini rejects.
	req := Request{
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: ""}}},
		},
	}
	g := buildGeminiRequest(req)
	if len(g.Contents) != 0 {
		t.Errorf("empty-text message should be dropped, got %+v", g.Contents)
	}
}

func TestGeminiStream_E2E(t *testing.T) {
	// fake SSE server emitting two text chunks then finishReason
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello \"}],\"role\":\"model\"}}]}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"world\"}],\"role\":\"model\"},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":3}}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewGemini(GeminiConfig{BaseURL: srv.URL, APIKey: ""})
	ch, err := p.Stream(context.Background(), Request{Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	var done bool
	var usage *Usage
	var stop string
	for ev := range ch {
		switch ev.Type {
		case EventTextDelta:
			got.WriteString(ev.Delta)
		case EventDone:
			done = true
			usage = ev.Usage
			stop = ev.StopReason
		case EventError:
			t.Fatal(ev.Err)
		}
	}
	if got.String() != "hello world" {
		t.Errorf("got %q", got.String())
	}
	if !done {
		t.Error("expected done event")
	}
	if stop != "STOP" {
		t.Errorf("stop reason: got %q", stop)
	}
	if usage == nil || usage.InputTokens != 5 || usage.OutputTokens != 3 {
		t.Errorf("usage missing or wrong: %+v", usage)
	}
}

func TestGeminiStream_ToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"shell\",\"args\":{\"cmd\":\"ls\"}}}],\"role\":\"model\"},\"finishReason\":\"STOP\"}]}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewGemini(GeminiConfig{BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	var tu *types.ToolUse
	for ev := range ch {
		if ev.Type == EventToolUse {
			tu = ev.ToolUse
		}
		if ev.Type == EventError {
			t.Fatal(ev.Err)
		}
	}
	if tu == nil || tu.Name != "shell" {
		t.Fatalf("expected tool use shell, got %+v", tu)
	}
	if tu.Input["cmd"] != "ls" {
		t.Errorf("args: %+v", tu.Input)
	}
	if tu.ID == "" {
		t.Errorf("tool use id should be generated")
	}
}

func TestGeminiStream_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad model"}`)
	}))
	defer srv.Close()

	p := NewGemini(GeminiConfig{BaseURL: srv.URL})
	_, err := p.Stream(context.Background(), Request{Model: "x"})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if !strings.Contains(err.Error(), "gemini 400") {
		t.Errorf("error message wrong: %v", err)
	}
}

func TestGeminiURL_KeyAppendedWhenPresent(t *testing.T) {
	// capture the path the provider hits so we can verify the key is appended.
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.RequestURI()
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"\"}],\"role\":\"model\"},\"finishReason\":\"STOP\"}]}\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	p := NewGemini(GeminiConfig{BaseURL: srv.URL, APIKey: "secret"})
	ch, err := p.Stream(context.Background(), Request{Model: "gemini-2.0-flash"})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if !strings.Contains(gotURL, "models/gemini-2.0-flash:streamGenerateContent") {
		t.Errorf("path wrong: %s", gotURL)
	}
	if !strings.Contains(gotURL, "alt=sse") {
		t.Errorf("missing alt=sse: %s", gotURL)
	}
	if !strings.Contains(gotURL, "key=secret") {
		t.Errorf("missing key param: %s", gotURL)
	}
}

func TestGeminiName(t *testing.T) {
	p := NewGemini(GeminiConfig{})
	if p.Name() != "gemini" {
		t.Errorf("name: got %q", p.Name())
	}
}
