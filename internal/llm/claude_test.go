package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/types"
)

func TestClaude_APIKey_HeadersAndStream(t *testing.T) {
	t.Setenv("CLAUDE_TEST_KEY", "sk-ant-api-xyz")

	var gotAPIKey, gotAuth, gotBeta, gotUA, gotVer, gotXApp string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("x-api-key")
		gotAuth = r.Header.Get("Authorization")
		gotBeta = r.Header.Get("anthropic-beta")
		gotUA = r.Header.Get("User-Agent")
		gotVer = r.Header.Get("anthropic-version")
		gotXApp = r.Header.Get("x-app")

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		write := func(payload any) {
			b, _ := json.Marshal(payload)
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
		write(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"id": "m1", "usage": map[string]any{"input_tokens": 5, "output_tokens": 0}},
		})
		write(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "hi"},
		})
		write(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"input_tokens": 5, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	p := NewClaude(ClaudeConfig{
		Name:    "anthropic",
		BaseURL: srv.URL,
		EnvKey:  "CLAUDE_TEST_KEY",
	})
	ch, err := p.Stream(context.Background(), Request{
		Model:    "claude-sonnet-4-6",
		Stream:   true,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotText string
	var done Event
	for ev := range ch {
		if ev.Type == EventTextDelta {
			gotText += ev.Delta
		}
		if ev.Type == EventDone {
			done = ev
		}
	}

	if gotText != "hi" {
		t.Errorf("text = %q want 'hi'", gotText)
	}
	if done.Usage == nil || done.Usage.OutputTokens != 1 {
		t.Errorf("usage missing/wrong: %+v", done.Usage)
	}
	if gotAPIKey != "sk-ant-api-xyz" {
		t.Errorf("x-api-key = %q", gotAPIKey)
	}
	if gotUA != claudeUserAgent {
		t.Errorf("user-agent = %q want %q", gotUA, claudeUserAgent)
	}
	if gotVer != claudeAPIVersion {
		t.Errorf("anthropic-version = %q", gotVer)
	}
	// first-party-client identity headers must NEVER appear.
	if gotAuth != "" {
		t.Errorf("api-key path must not set Authorization: %q", gotAuth)
	}
	if gotBeta != "" {
		t.Errorf("api-key path must not set anthropic-beta: %q", gotBeta)
	}
	if gotXApp != "" {
		t.Errorf("api-key path must not set x-app: %q", gotXApp)
	}
	if strings.Contains(gotUA, "claude") {
		t.Errorf("user-agent must not impersonate claude: %q", gotUA)
	}
}

func TestClaude_APIKey_CacheControlAndNoRemap(t *testing.T) {
	t.Setenv("CLAUDE_TEST_KEY", "sk-ant-api-foo")

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer srv.Close()

	p := NewClaude(ClaudeConfig{
		Name:    "anthropic",
		BaseURL: srv.URL,
		EnvKey:  "CLAUDE_TEST_KEY",
	})
	ch, err := p.Stream(context.Background(), Request{
		Model:    "claude-sonnet-4-6",
		System:   "be brief",
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}}},
		Tools:    []ToolSpec{{Name: "shell", Schema: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	body := string(gotBody)
	if !strings.Contains(body, `"cache_control":{"type":"ephemeral"}`) {
		t.Errorf("expected ephemeral cache_control on system + last tool: %s", body)
	}
	// system breakpoint must sit inside the system block
	if !strings.Contains(body, `"system":[`) || !strings.Contains(body, `"text":"be brief"`) {
		t.Errorf("system block missing expected shape: %s", body)
	}
	if strings.Contains(body, `"name":"Bash"`) {
		t.Errorf("api-key path must not remap shell→Bash: %s", body)
	}
	if !strings.Contains(body, `"name":"shell"`) {
		t.Errorf("native tool name 'shell' missing: %s", body)
	}
	if strings.Contains(body, "You are Claude Code") {
		t.Errorf("api-key path must not inject first-party-client identity: %s", body)
	}
}

func TestClaude_ToolUseRoundtripInStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		write := func(payload any) {
			b, _ := json.Marshal(payload)
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
		write(map[string]any{"type": "message_start", "message": map[string]any{"id": "m1"}})
		write(map[string]any{
			"type":          "content_block_start",
			"index":         0,
			"content_block": map[string]any{"type": "tool_use", "id": "toolu_1", "name": "shell"},
		})
		write(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"cmd":"l`},
		})
		write(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `s"}`},
		})
		write(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "tool_use"},
			"usage": map[string]any{"output_tokens": 2},
		})
	}))
	defer srv.Close()

	p := NewClaude(ClaudeConfig{Name: "anthropic", BaseURL: srv.URL, EnvKey: "_NOPE"})
	ch, _ := p.Stream(context.Background(), Request{Model: "m", Stream: true})
	var got *types.ToolUse
	for ev := range ch {
		if ev.Type == EventToolUse {
			got = ev.ToolUse
		}
	}
	if got == nil {
		t.Fatal("tool use missing")
	}
	if got.ID != "toolu_1" || got.Name != "shell" {
		t.Errorf("tool meta wrong: %+v", got)
	}
	if got.Input["cmd"] != "ls" {
		t.Errorf("tool input not assembled from delta: %+v", got.Input)
	}
}
