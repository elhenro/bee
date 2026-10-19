package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elhenro/bee/internal/auth"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// streams one request through buildProvider and returns the named header the
// endpoint saw.
func headerSeen(t *testing.T, cfg config.Config, header string) string {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(header)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()
	pc := cfg.Providers[cfg.DefaultProvider]
	pc.BaseURL = srv.URL
	cfg.Providers[cfg.DefaultProvider] = pc
	p, err := buildProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := p.Stream(context.Background(), llm.Request{
		Model: "m", Stream: true,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentBlock{{Type: types.BlockText, Text: "x"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	return got
}

// cfg.APIKey belongs to the provider chosen at load; after a /model switch or
// a swarm tier the selected provider must use its own key.
func TestGeminiUsesSelectedProviderKey(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "gemini-key")
	cfg := config.Defaults()
	cfg.APIKey = "key-of-the-original-provider"
	cfg.DefaultProvider = "gemini"
	if got := headerSeen(t, cfg, "x-goog-api-key"); got != "gemini-key" {
		t.Fatalf("gemini sent key %q, want its own env key", got)
	}
}

func TestGeminiFallsBackToStoredKey(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("GEMINI_API_KEY", "")
	dir, _ := auth.DefaultDir()
	if err := auth.SaveAPIKey(dir, "gemini", "stored-gemini"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.APIKey = "key-of-the-original-provider"
	cfg.DefaultProvider = "gemini"
	if got := headerSeen(t, cfg, "x-goog-api-key"); got != "stored-gemini" {
		t.Fatalf("gemini sent key %q, want stored key", got)
	}
}

func TestAnthropicMessagesHonoursStoredKey(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	t.Setenv("ANTHROPIC_API_KEY", "")
	dir, _ := auth.DefaultDir()
	if err := auth.SaveAPIKey(dir, "anthropic", "sk-stored"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.DefaultProvider = "anthropic"
	if got := headerSeen(t, cfg, "x-api-key"); got != "sk-stored" {
		t.Fatalf("anthropic sent key %q, want stored key", got)
	}
}
