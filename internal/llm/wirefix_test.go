package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/types"
)

func sseWrite(w http.ResponseWriter, payloads ...any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, p := range payloads {
		b, _ := json.Marshal(p)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
	}
}

func drainEvents(ch <-chan Event) []Event {
	var evs []Event
	for ev := range ch {
		evs = append(evs, ev)
	}
	return evs
}

func fastRetry(t *testing.T) {
	old := retryDelay
	retryDelay = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() { retryDelay = old })
}

func TestClaudeStreamCarriesThinkingSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseWrite(w,
			map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking"}},
			map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "hm"}},
			map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "sig-abc"}},
			map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}},
		)
	}))
	defer srv.Close()
	p := NewClaude(ClaudeConfig{BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Model: "claude-sonnet-4-5", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	sig := ""
	for _, ev := range drainEvents(ch) {
		if ev.Type == EventThinkingDelta && ev.Signature != "" {
			sig = ev.Signature
		}
	}
	if sig != "sig-abc" {
		t.Errorf("signature = %q", sig)
	}
}

func TestClaudeStreamBadToolJSONFlagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseWrite(w,
			map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "t1", "name": "write"}},
			map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"path":"a","content":"tru`}},
			map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "max_tokens"}},
		)
	}))
	defer srv.Close()
	p := NewClaude(ClaudeConfig{BaseURL: srv.URL})
	ch, _ := p.Stream(context.Background(), Request{Model: "claude-sonnet-4-5", Stream: true})
	for _, ev := range drainEvents(ch) {
		if ev.Type == EventToolUse {
			if _, ok := ev.ToolUse.Input["_parse_error"]; !ok {
				t.Errorf("truncated args must carry _parse_error, got %v", ev.ToolUse.Input)
			}
			return
		}
	}
	t.Fatal("no tool use")
}

func TestClaudeRetriesOverloaded(t *testing.T) {
	fastRetry(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) < 3 {
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error"}`))
			return
		}
		sseWrite(w, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}})
	}))
	defer srv.Close()
	p := NewClaude(ClaudeConfig{BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Model: "claude-sonnet-4-5", Stream: true})
	if err != nil {
		t.Fatalf("overloaded should retry: %v", err)
	}
	drainEvents(ch)
	if hits.Load() != 3 {
		t.Errorf("hits = %d want 3", hits.Load())
	}
}

func TestPostWithRetryDoesNotRetryClientError(t *testing.T) {
	fastRetry(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(400)
	}))
	defer srv.Close()
	p := NewClaude(ClaudeConfig{BaseURL: srv.URL})
	if _, err := p.Stream(context.Background(), Request{Model: "m", Stream: true}); err == nil {
		t.Fatal("want error")
	}
	if hits.Load() != 1 {
		t.Errorf("400 must not retry, hits = %d", hits.Load())
	}
}

func TestGeminiRequestFunctionResponseName(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleAssistant, Content: []types.ContentBlock{
			{Type: types.BlockToolUse, Use: &types.ToolUse{ID: "call_9", Name: "read", Input: map[string]any{"p": "x"}, Signature: "ts"}},
		}},
		{Role: types.RoleTool, Content: []types.ContentBlock{
			{Type: types.BlockToolResult, Result: &types.ToolResult{UseID: "call_9", Content: "ok"}},
		}},
	}
	req := buildGeminiRequest(Request{Model: "gemini-2.5-flash", Messages: msgs})
	fr := req.Contents[1].Parts[0].FunctionResponse
	if fr == nil || fr.Name != "read" {
		t.Errorf("functionResponse name = %+v want read", fr)
	}
	if got := req.Contents[0].Parts[0].ThoughtSignature; got != "ts" {
		t.Errorf("thoughtSignature = %q want ts", got)
	}
}

func TestGeminiThinkingBudgetCapped(t *testing.T) {
	for model, want := range map[string]int{
		"gemini-2.5-flash": 24576,
		"gemini-2.5-pro":   32768,
	} {
		req := buildGeminiRequest(Request{Model: model, Thinking: ThinkingMax})
		tc := req.GenerationConfig["thinkingConfig"].(map[string]any)
		if tc["thinkingBudget"] != want {
			t.Errorf("%s: budget = %v want %d", model, tc["thinkingBudget"], want)
		}
	}
	req := buildGeminiRequest(Request{Model: "gemini-2.5-flash", Thinking: ThinkingLow})
	if tc := req.GenerationConfig["thinkingConfig"].(map[string]any); tc["thinkingBudget"] != 1024 {
		t.Errorf("low budget must pass through, got %v", tc["thinkingBudget"])
	}
}

func TestGeminiStreamSignatureAndThoughtTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseWrite(w, map[string]any{
			"candidates": []any{map[string]any{
				"content":      map[string]any{"parts": []any{map[string]any{"functionCall": map[string]any{"name": "read", "args": map[string]any{}}, "thoughtSignature": "sg"}}},
				"finishReason": "STOP",
			}},
			"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 5, "thoughtsTokenCount": 7},
		})
	}))
	defer srv.Close()
	p := NewGemini(GeminiConfig{BaseURL: srv.URL, APIKey: "k"})
	ch, err := p.Stream(context.Background(), Request{Model: "gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	var sig string
	var out int
	for _, ev := range drainEvents(ch) {
		if ev.Type == EventToolUse {
			sig = ev.ToolUse.Signature
		}
		if ev.Type == EventDone && ev.Usage != nil {
			out = ev.Usage.OutputTokens
		}
	}
	if sig != "sg" {
		t.Errorf("tool signature = %q", sig)
	}
	if out != 12 {
		t.Errorf("output tokens = %d want 12 (candidates + thoughts)", out)
	}
}

func TestChatGPTStreamTopLevelErrorAndIncomplete(t *testing.T) {
	run := func(payloads ...any) []Event {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sseWrite(w, payloads...) }))
		defer srv.Close()
		withHomeDir(t)
		p := NewChatGPT(ChatGPTConfig{Name: "chatgpt", BaseURL: srv.URL})
		ch, err := p.Stream(context.Background(), Request{Model: "m", Stream: true})
		if err != nil {
			t.Fatal(err)
		}
		return drainEvents(ch)
	}
	evs := run(map[string]any{"type": "error", "code": "rate_limit", "message": "slow down"})
	last := evs[len(evs)-1]
	if last.Type != EventError || !strings.Contains(last.Err.Error(), "slow down") {
		t.Errorf("top-level error must surface, got %+v", last)
	}
	evs = run(map[string]any{"type": "response.incomplete", "response": map[string]any{
		"status": "incomplete", "incomplete_details": map[string]any{"reason": "max_output_tokens"},
		"usage": map[string]any{"input_tokens": 3, "output_tokens": 4},
	}})
	last = evs[len(evs)-1]
	if last.Type != EventDone || last.StopReason != "length" || last.Usage == nil || last.Usage.OutputTokens != 4 {
		t.Errorf("incomplete must end as length with usage, got %+v", last)
	}
}

func TestChatGPTStreamBadToolJSONFlagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseWrite(w,
			map[string]any{"type": "response.output_item.added", "item": map[string]any{"id": "i1", "type": "function_call", "call_id": "c1", "name": "write"}},
			map[string]any{"type": "response.function_call_arguments.delta", "item_id": "i1", "delta": `{"path":`},
			map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed"}},
		)
	}))
	defer srv.Close()
	withHomeDir(t)
	p := NewChatGPT(ChatGPTConfig{Name: "chatgpt", BaseURL: srv.URL})
	ch, _ := p.Stream(context.Background(), Request{Model: "m", Stream: true})
	for _, ev := range drainEvents(ch) {
		if ev.Type == EventToolUse {
			if _, ok := ev.ToolUse.Input["_parse_error"]; !ok {
				t.Errorf("want _parse_error, got %v", ev.ToolUse.Input)
			}
			return
		}
	}
	t.Fatal("no tool use")
}

func TestEmitDropsOnCancelledFullChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan Event, 1)
	if !emit(ctx, out, Event{Type: EventTextDelta}) {
		t.Fatal("free slot must accept")
	}
	cancel()
	done := make(chan bool)
	go func() { done <- emit(ctx, out, Event{Type: EventTextDelta}) }()
	select {
	case ok := <-done:
		if ok {
			t.Error("full channel on cancelled ctx must drop")
		}
	case <-time.After(time.Second):
		t.Fatal("emit blocked after cancel")
	}
}

func TestOpenAIStreamStallHonorsEnv(t *testing.T) {
	t.Setenv("BEE_STREAM_STALL_SECONDS", "1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	p := NewOpenAICompat(OpenAICompatConfig{BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Model: "m", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Type != EventError || !strings.Contains(ev.Err.Error(), "stalled") || !strings.Contains(ev.Err.Error(), "1s") {
			t.Errorf("want stall error naming 1s, got %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("env stall window ignored")
	}
}
