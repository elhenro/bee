package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

func TestProbeContextLength_ShortCircuitsOnKnown(t *testing.T) {
	ResetLiveContextLengths()
	ResetProbed()
	RememberContextLength("llama3.1:8b", 131072)

	// http client that errors any request so we can prove no HTTP happened.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	SetProbeHTTPClient(srv.Client())

	got := ProbeContextLength(context.Background(), "ollama", config.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		WireAPI: "chat",
	}, "llama3.1:8b")

	if got != 131072 {
		t.Errorf("want cached 131072, got %d", got)
	}
	if calls != 0 {
		t.Errorf("known model should not hit upstream, calls=%d", calls)
	}
}

func TestProbeContextLength_OllamaShow(t *testing.T) {
	ResetLiveContextLengths()
	ResetProbed()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "qwen2.5-coder:7b" {
			http.Error(w, "wrong model", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model_info": map[string]any{
				"general.architecture":   "qwen2",
				"qwen2.context_length":   32768.0,
				"qwen2.embedding_length": 3584.0,
			},
		})
	}))
	defer srv.Close()
	SetProbeHTTPClient(srv.Client())

	got := ProbeContextLength(context.Background(), "ollama", config.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		WireAPI: "chat",
	}, "qwen2.5-coder:7b")

	if got != 32768 {
		t.Errorf("want 32768 from model_info, got %d", got)
	}
	if ContextWindow("qwen2.5-coder:7b") != 32768 {
		t.Errorf("want cached context window 32768, got %d", ContextWindow("qwen2.5-coder:7b"))
	}
}

func TestProbeContextLength_OllamaShowFailsGracefully(t *testing.T) {
	ResetLiveContextLengths()
	ResetProbed()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	SetProbeHTTPClient(srv.Client())

	got := ProbeContextLength(context.Background(), "ollama", config.ProviderConfig{
		BaseURL: srv.URL + "/v1",
		WireAPI: "chat",
	}, "unknown-model")
	if got != 0 {
		t.Errorf("want 0 on probe failure, got %d", got)
	}
}

func TestProbeContextLength_OpenAICompatModelsEndpoint(t *testing.T) {
	ResetLiveContextLengths()
	ResetProbed()
	ClearModelCache()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "custom-model-7b", "name": "Custom 7B", "context_length": 65536},
			},
		})
	}))
	defer srv.Close()
	SetProbeHTTPClient(srv.Client())
	SetModelsHTTPClient(srv.Client())

	got := ProbeContextLength(context.Background(), "openrouter", config.ProviderConfig{
		BaseURL: srv.URL,
		WireAPI: "chat",
	}, "custom-model-7b")
	if got != 65536 {
		t.Errorf("want 65536 from /v1/models, got %d", got)
	}
}

func TestProbeContextLength_SkipsAnthropicWire(t *testing.T) {
	ResetLiveContextLengths()
	ResetProbed()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()
	SetProbeHTTPClient(srv.Client())

	_ = ProbeContextLength(context.Background(), "anthropic", config.ProviderConfig{
		BaseURL: srv.URL,
		WireAPI: "anthropic-messages",
	}, "claude-opus-9999")
	if calls != 0 {
		t.Errorf("anthropic wire should be skipped, calls=%d", calls)
	}
}

func TestProbeContextLength_DedupesPerModel(t *testing.T) {
	ResetLiveContextLengths()
	ResetProbed()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	SetProbeHTTPClient(srv.Client())

	cfg := config.ProviderConfig{BaseURL: srv.URL + "/v1", WireAPI: "chat"}
	_ = ProbeContextLength(context.Background(), "ollama", cfg, "fake-model")
	_ = ProbeContextLength(context.Background(), "ollama", cfg, "fake-model")
	_ = ProbeContextLength(context.Background(), "ollama", cfg, "fake-model")

	// only the first call should reach upstream
	if calls > 2 { // 1 for /api/show + maybe 1 for /v1/models on first attempt
		t.Errorf("want dedupe after first probe, calls=%d", calls)
	}
}
