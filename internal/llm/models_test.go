package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/config"
)

func TestListModels_HTTPHappyPath(t *testing.T) {
	ClearModelCache()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-x" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[
			{"id":"openai/gpt-4o","name":"GPT-4o","context_length":128000,"pricing":{"prompt":"0.005","completion":"0.015"}},
			{"id":"anthropic/claude-haiku","name":"Claude Haiku","context_length":200000}
		]}`)
	}))
	defer srv.Close()

	t.Setenv("OPENROUTER_API_KEY", "sk-x")
	cfg := config.ProviderConfig{BaseURL: srv.URL, WireAPI: "chat", EnvKey: "OPENROUTER_API_KEY"}

	got, err := ListModels(context.Background(), "openrouter", cfg)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// sorted by id, so anthropic/* before openai/*
	if got[0].ID != "anthropic/claude-haiku" {
		t.Errorf("sort order wrong: %+v", got)
	}
	if got[1].Pricing != "in:0.005 out:0.015" {
		t.Errorf("pricing = %q", got[1].Pricing)
	}
}

func TestListModels_CacheTTL(t *testing.T) {
	ClearModelCache()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// only count /models — /models/status is a separate probe by
		// enrichContextLengths that's unrelated to cache behavior.
		if r.URL.Path == "/models" {
			atomic.AddInt32(&hits, 1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"m1","name":"M1"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{BaseURL: srv.URL, WireAPI: "chat"}

	for i := 0; i < 3; i++ {
		if _, err := ListModels(context.Background(), "p", cfg); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("hits = %d, want 1 (cache should suppress)", n)
	}

	// poke TTL: rewrite cache entry to be older than ttl
	modelCacheMu.Lock()
	e := modelCache["p"]
	e.at = time.Now().Add(-2 * modelCacheTTL)
	modelCache["p"] = e
	modelCacheMu.Unlock()

	if _, err := ListModels(context.Background(), "p", cfg); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Fatalf("hits = %d, want 2 after ttl expiry", n)
	}
}

func TestListModels_AnthropicHardcoded(t *testing.T) {
	ClearModelCache()
	cfg := config.ProviderConfig{BaseURL: "https://api.anthropic.com/v1", WireAPI: "anthropic"}
	got, err := ListModels(context.Background(), "anthropic", cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("expected hardcoded anthropic list, got 0")
	}
	want := "claude-sonnet-4-6"
	found := false
	for _, m := range got {
		if m.ID == want {
			found = true
		}
	}
	if !found {
		t.Errorf("missing %q in hardcoded list: %+v", want, got)
	}
}

func TestListModels_EmptyBaseURL(t *testing.T) {
	ClearModelCache()
	cfg := config.ProviderConfig{BaseURL: "", WireAPI: "chat"}
	got, err := ListModels(context.Background(), "weird", cfg)
	if err != nil {
		t.Fatal(err)
	}
	// chat wire with empty base url returns empty slice (no fallback for "weird")
	if got == nil || len(got) != 0 {
		t.Errorf("got = %+v, want empty", got)
	}
}

// omlx /v1/models returns no context_length, but /v1/models/status does.
// Verify enrichContextLengths backfills ContextLength so ContextWindow can
// answer for MLX models.
func TestListModels_OmlxStatusEnrichesContextLength(t *testing.T) {
	ClearModelCache()
	ResetLiveContextLengths()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			fmt.Fprint(w, `{"data":[
				{"id":"Qwen3.6-27B-8bit","object":"model","owned_by":"omlx"},
				{"id":"Qwen3.6-35B-A3B-4bit","object":"model","owned_by":"omlx"}
			]}`)
		case "/models/status":
			fmt.Fprint(w, `{"models":[
				{"id":"Qwen3.6-27B-8bit","max_context_window":127996,"max_tokens":128000},
				{"id":"Qwen3.6-35B-A3B-4bit","max_context_window":127996,"max_tokens":128000}
			]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{BaseURL: srv.URL, WireAPI: "chat"}
	got, err := ListModels(context.Background(), "omlx", cfg)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	for _, m := range got {
		if m.ContextLength != 127996 {
			t.Errorf("%s ContextLength = %d, want 127996", m.ID, m.ContextLength)
		}
	}
	if c := ContextWindow("Qwen3.6-27B-8bit"); c != 127996 {
		t.Errorf("ContextWindow live cache = %d, want 127996", c)
	}
}

// status endpoint missing (404): we still return the plain /models list,
// just without ContextLength enrichment.
func TestListModels_OmlxStatusMissingIsBenign(t *testing.T) {
	ClearModelCache()
	ResetLiveContextLengths()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"local-model","object":"model"}]}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{BaseURL: srv.URL, WireAPI: "chat"}
	got, err := ListModels(context.Background(), "omlx", cfg)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(got) != 1 || got[0].ContextLength != 0 {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestListModels_FallbackOnHTTPError(t *testing.T) {
	ClearModelCache()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	cfg := config.ProviderConfig{BaseURL: srv.URL, WireAPI: "chat", EnvKey: ""}

	got, err := ListModels(context.Background(), "openai", cfg)
	if err != nil {
		t.Fatalf("expected fallback to swallow error, got: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected openai fallback list, got empty")
	}
}
