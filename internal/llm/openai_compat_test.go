package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/types"
)

func newTestProvider(t *testing.T, h http.HandlerFunc) (*OpenAICompatProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Setenv("TEST_KEY", "sk-fake")
	return NewOpenAICompat(OpenAICompatConfig{
		Name:    "test",
		BaseURL: srv.URL,
		EnvKey:  "TEST_KEY",
	}), srv
}

func drain(t *testing.T, ch <-chan Event, deadline time.Duration) []Event {
	t.Helper()
	var got []Event
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return got
			}
			got = append(got, ev)
		case <-timer.C:
			t.Fatalf("timed out waiting for events; got so far: %+v", got)
		}
	}
}

func TestOpenAICompat_NonStreamingHappyPath(t *testing.T) {
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-fake" {
			t.Errorf("auth header: got %q", got)
		}
		if got := r.Header.Get("HTTP-Referer"); got != "https://github.com/elhenro/bee" {
			t.Errorf("referer: got %q", got)
		}
		if got := r.Header.Get("X-Title"); got != "bee" {
			t.Errorf("title: got %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"gpt-4o-mini"`) {
			t.Errorf("body missing model: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"choices":[{
				"message":{"content":"hello world","tool_calls":[]},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}
		}`)
	})

	ctx := context.Background()
	ch, err := p.Stream(ctx, Request{
		Model:    "gpt-4o-mini",
		System:   "be brief",
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	events := drain(t, ch, 2*time.Second)
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Type != EventTextDelta || events[0].Delta != "hello world" {
		t.Errorf("first event: %+v", events[0])
	}
	if events[1].Type != EventDone || events[1].StopReason != "stop" {
		t.Errorf("last event: %+v", events[1])
	}
	if events[1].Usage == nil || events[1].Usage.InputTokens != 4 || events[1].Usage.OutputTokens != 2 {
		t.Errorf("usage: %+v", events[1].Usage)
	}
}

func TestOpenAICompat_StreamingWithToolCall(t *testing.T) {
	chunks := []string{
		`data: {"id":"a","choices":[{"index":0,"delta":{"role":"assistant","content":"working"}}]}`,
		`data: {"id":"a","choices":[{"index":0,"delta":{"content":" now"}}]}`,
		`data: {"id":"a","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"shell"}}]}}]}`,
		`data: {"id":"a","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"cmd\":\"ls\"}"}}]}}]}`,
		`data: {"id":"a","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`,
		`data: [DONE]`,
	}

	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			fmt.Fprint(w, c+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	})

	ch, err := p.Stream(context.Background(), Request{Model: "m", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, ch, 2*time.Second)

	var text strings.Builder
	var toolUse *types.ToolUse
	var done *Event
	for i := range events {
		ev := events[i]
		switch ev.Type {
		case EventTextDelta:
			text.WriteString(ev.Delta)
		case EventToolUse:
			toolUse = ev.ToolUse
		case EventDone:
			done = &events[i]
		case EventError:
			t.Fatalf("unexpected error: %v", ev.Err)
		}
	}
	if text.String() != "working now" {
		t.Errorf("text: %q", text.String())
	}
	if toolUse == nil {
		t.Fatal("no tool use emitted")
	}
	if toolUse.ID != "call_x" || toolUse.Name != "shell" || toolUse.Input["cmd"] != "ls" {
		t.Errorf("tool use: %+v", toolUse)
	}
	if done == nil || done.StopReason != "tool_calls" {
		t.Errorf("done: %+v", done)
	}
	if done.Usage == nil || done.Usage.InputTokens != 10 {
		t.Errorf("usage: %+v", done.Usage)
	}
}

func TestOpenAICompat_ContextCancelMidStream(t *testing.T) {
	// server writes one chunk, then holds the connection until told to release
	release := make(chan struct{})
	var once sync.Once
	p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"id":"a","choices":[{"index":0,"delta":{"content":"x"}}]}`+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		// block until test releases
		select {
		case <-release:
		case <-r.Context().Done():
		}
		once.Do(func() { close(release) })
	})

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := p.Stream(ctx, Request{Model: "m", Stream: true})
	if err != nil {
		t.Fatal(err)
	}

	// read first delta then cancel
	first := <-ch
	if first.Type != EventTextDelta || first.Delta != "x" {
		t.Fatalf("first event: %+v", first)
	}
	cancel()

	// expect an error event then channel close, within a short window
	deadline := time.After(2 * time.Second)
	gotErr := false
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				if !gotErr {
					t.Fatal("channel closed without error event")
				}
				return
			}
			if ev.Type == EventError {
				gotErr = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for cancel propagation")
		}
	}
}

func TestOpenAICompat_HTTPErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"401 auth", http.StatusUnauthorized, `{"error":{"message":"bad key"}}`},
		{"429 ratelimit", http.StatusTooManyRequests, `{"error":{"message":"slow down"}}`},
		{"500 server", http.StatusInternalServerError, `oops`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, err := p.Stream(context.Background(), Request{Model: "m"})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("status %d", tc.status)) {
				t.Errorf("error should mention status %d, got: %v", tc.status, err)
			}
		})
	}
}

func TestOpenAICompat_NameDefault(t *testing.T) {
	p := NewOpenAICompat(OpenAICompatConfig{BaseURL: "http://x"})
	if p.Name() != "openai-compat" {
		t.Errorf("default name: %q", p.Name())
	}
}

func TestOpenAICompat_ExtraHeaders(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Custom")
		fmt.Fprint(w, `{"choices":[{"message":{"content":""},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()
	p := NewOpenAICompat(OpenAICompatConfig{
		BaseURL:      srv.URL,
		ExtraHeaders: map[string]string{"X-Custom": "yes"},
	})
	ch, err := p.Stream(context.Background(), Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, ch, time.Second)
	if got != "yes" {
		t.Errorf("custom header: %q", got)
	}
}

func TestOpenAICompat_ReasoningEffort(t *testing.T) {
	cases := []struct {
		name  string
		level Thinking
		want  string // substring to look for in body
		empty bool   // true: field should be absent
	}{
		{"off omits", ThinkingOff, "", true},
		{"empty omits", "", "", true},
		{"low", ThinkingLow, `"reasoning_effort":"low"`, false},
		{"medium", ThinkingMedium, `"reasoning_effort":"medium"`, false},
		{"high", ThinkingHigh, `"reasoning_effort":"high"`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body string
			p, _ := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				body = string(b)
				fmt.Fprint(w, `{"choices":[{"message":{"content":""},"finish_reason":"stop"}]}`)
			})
			ch, err := p.Stream(context.Background(), Request{Model: "m", Thinking: tc.level})
			if err != nil {
				t.Fatal(err)
			}
			drain(t, ch, time.Second)
			if tc.empty {
				if strings.Contains(body, "reasoning_effort") {
					t.Errorf("expected reasoning_effort to be omitted; body: %s", body)
				}
				return
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("expected %s in body; got: %s", tc.want, body)
			}
		})
	}
}

func TestAnthropic_StubErrors(t *testing.T) {
	p := NewAnthropic("ANTHROPIC_API_KEY", "")
	if p.Name() != "anthropic" {
		t.Errorf("name: %q", p.Name())
	}
	_, err := p.Stream(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected stub to return error")
	}
}

func TestAnthropic_TranslateImageBlock(t *testing.T) {
	raw := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a} // PNG magic
	block := types.ContentBlock{
		Type:      types.BlockImage,
		MediaType: "image/png",
		Data:      raw,
	}
	out := translateAnthropicBlock(block)
	if out == nil {
		t.Fatal("image translation returned nil")
	}
	if out["type"] != "image" {
		t.Errorf("type: %v", out["type"])
	}
	src, ok := out["source"].(map[string]any)
	if !ok {
		t.Fatalf("source missing or wrong shape: %+v", out)
	}
	if src["type"] != "base64" || src["media_type"] != "image/png" {
		t.Errorf("source meta wrong: %+v", src)
	}
	if src["data"] == "" {
		t.Errorf("base64 data empty")
	}
}

func TestAnthropic_TranslateImageDefaultsMediaType(t *testing.T) {
	out := translateAnthropicBlock(types.ContentBlock{Type: types.BlockImage, Data: []byte{1, 2, 3}})
	src := out["source"].(map[string]any)
	if src["media_type"] != "image/png" {
		t.Errorf("expected default media_type image/png, got %v", src["media_type"])
	}
}
