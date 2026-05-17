package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/auth"
	"github.com/elhenro/bee/internal/types"
)

// withHomeDir redirects ~/.bee/auth to t.TempDir() for the test by setting
// HOME (LoadToken uses os.UserHomeDir).
func withHomeDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// also honor XDG conventions on linux just in case
	t.Setenv("USERPROFILE", tmp)
	return filepath.Join(tmp, ".bee", "auth")
}

func saveTok(t *testing.T, dir, provider string, tok *auth.Token) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := auth.SaveToken(dir, provider, tok); err != nil {
		t.Fatal(err)
	}
}

func TestChatGPT_Stream_TextAndUsage(t *testing.T) {
	dir := withHomeDir(t)
	// pre-populate the auth dir
	d, err := auth.DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if d != dir {
		t.Fatalf("DefaultDir = %q want %q", d, dir)
	}
	saveTok(t, dir, "chatgpt", &auth.Token{AccessToken: "AT", AccountID: "acct-1", ExpiresIn: 3600})

	var gotAuth, gotAcct string
	srv, err := func() (*httptest.Server, error) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			gotAcct = r.Header.Get("chatgpt-account-id")
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			write := func(payload any) {
				b, _ := json.Marshal(payload)
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(b)
				_, _ = w.Write([]byte("\n\n"))
				flusher.Flush()
			}
			write(map[string]any{"type": "response.output_text.delta", "delta": "hello "})
			write(map[string]any{"type": "response.output_text.delta", "delta": "world"})
			write(map[string]any{
				"type": "response.completed",
				"response": map[string]any{
					"id": "r_1", "status": "completed",
					"usage": map[string]any{"input_tokens": 10, "output_tokens": 2},
				},
			})
		}))
		return s, nil
	}()
	defer srv.Close()

	p := NewChatGPT(ChatGPTConfig{
		Name:            "chatgpt",
		BaseURL:         srv.URL,
		AccountIDHeader: "chatgpt-account-id",
	})
	ch, err := p.Stream(context.Background(), Request{Model: "gpt-5-codex", Messages: []types.Message{
		{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "hi"}}},
	}, Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var usage *Usage
	for ev := range ch {
		switch ev.Type {
		case EventTextDelta:
			text.WriteString(ev.Delta)
		case EventDone:
			usage = ev.Usage
		case EventError:
			t.Fatalf("error event: %v", ev.Err)
		}
	}
	if text.String() != "hello world" {
		t.Errorf("text = %q", text.String())
	}
	if usage == nil || usage.InputTokens != 10 || usage.OutputTokens != 2 {
		t.Errorf("usage = %+v", usage)
	}
	if gotAuth != "Bearer AT" {
		t.Errorf("auth header = %q", gotAuth)
	}
	if gotAcct != "acct-1" {
		t.Errorf("account-id header = %q", gotAcct)
	}
}

func TestChatGPT_Stream_FunctionCall(t *testing.T) {
	dir := withHomeDir(t)
	saveTok(t, dir, "chatgpt", &auth.Token{AccessToken: "AT", ExpiresIn: 3600})

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
		write(map[string]any{
			"type": "response.output_item.added",
			"item": map[string]any{
				"id": "item_1", "type": "function_call",
				"call_id": "call_xyz", "name": "shell",
			},
		})
		write(map[string]any{
			"type":    "response.function_call_arguments.delta",
			"item_id": "item_1",
			"delta":   `{"cmd":`,
		})
		write(map[string]any{
			"type":    "response.function_call_arguments.delta",
			"item_id": "item_1",
			"delta":   `"ls"}`,
		})
		write(map[string]any{
			"type":      "response.function_call_arguments.done",
			"item_id":   "item_1",
			"arguments": `{"cmd":"ls"}`,
		})
		write(map[string]any{
			"type": "response.completed",
			"response": map[string]any{"id": "r", "status": "completed"},
		})
	}))
	defer srv.Close()

	p := NewChatGPT(ChatGPTConfig{Name: "chatgpt", BaseURL: srv.URL})
	ch, err := p.Stream(context.Background(), Request{Model: "m", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	var got *types.ToolUse
	for ev := range ch {
		if ev.Type == EventToolUse {
			got = ev.ToolUse
		}
		if ev.Type == EventError {
			t.Fatalf("err: %v", ev.Err)
		}
	}
	if got == nil {
		t.Fatal("no tool use emitted")
	}
	if got.ID != "call_xyz" || got.Name != "shell" {
		t.Errorf("tool = %+v", got)
	}
	if got.Input["cmd"] != "ls" {
		t.Errorf("input = %+v", got.Input)
	}
}

func TestChatGPT_Stream_RefreshOn401(t *testing.T) {
	dir := withHomeDir(t)
	saveTok(t, dir, "chatgpt", &auth.Token{AccessToken: "OLD", RefreshToken: "RT", ExpiresIn: 1})

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("expired"))
			return
		}
		// second call: ensure we now carry the new bearer
		if r.Header.Get("Authorization") != "Bearer NEW" {
			t.Errorf("retry auth = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		b, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r", "status": "completed"}})
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(b)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	tokSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "NEW", "expires_in": 3600, "token_type": "Bearer"})
	}))
	defer tokSrv.Close()

	p := NewChatGPT(ChatGPTConfig{
		Name:          "chatgpt",
		BaseURL:       srv.URL,
		ClientID:      "cid",
		TokenEndpoint: tokSrv.URL,
	})
	ch, err := p.Stream(context.Background(), Request{Model: "m", Stream: true})
	if err != nil {
		t.Fatal(err)
	}
	for ev := range ch {
		if ev.Type == EventError {
			t.Fatalf("err: %v", ev.Err)
		}
	}
	if calls < 2 {
		t.Errorf("expected retry, calls = %d", calls)
	}
	// confirm the new token was persisted with RefreshToken preserved
	tok, _ := auth.LoadToken(dir, "chatgpt")
	if tok == nil || tok.AccessToken != "NEW" || tok.RefreshToken != "RT" {
		t.Errorf("token after refresh = %+v", tok)
	}
}
