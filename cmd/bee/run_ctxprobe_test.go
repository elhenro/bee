package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

func ctxProbeCfg(provider, model, baseURL string) config.Config {
	cfg := config.Defaults()
	cfg.DefaultProvider = provider
	cfg.DefaultModel = model
	cfg.Providers[provider] = config.ProviderConfig{BaseURL: baseURL, WireAPI: "chat", KeyOptional: true}
	return cfg
}

func resetCtxProbeState(t *testing.T) {
	t.Helper()
	reset := func() {
		llm.ResetLiveContextLengths()
		llm.ResetProbed()
		llm.ClearModelCache()
	}
	reset()
	t.Cleanup(reset)
}

// omlx has no /api/show; the window rides /v1/models as max_model_len.
func TestPrewarmLocalContext_OmlxMaxModelLen(t *testing.T) {
	resetCtxProbeState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"omlx-probe-model","max_model_len":262144},{"id":"MarkItDown","max_model_len":null}]}`))
	}))
	defer srv.Close()

	prewarmLocalContext(ctxProbeCfg("omlx", "omlx-probe-model", srv.URL+"/v1"))

	if got := llm.ContextWindow("omlx-probe-model"); got != 262144 {
		t.Fatalf("want 262144, got %d", got)
	}
}

// older omlx builds only report the window on /models/status.
func TestPrewarmLocalContext_OmlxModelsStatus(t *testing.T) {
	resetCtxProbeState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"omlx-status-model"}]}`))
		case "/v1/models/status":
			_, _ = w.Write([]byte(`{"models":[{"id":"omlx-status-model","max_context_window":131072}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prewarmLocalContext(ctxProbeCfg("omlx", "omlx-status-model", srv.URL+"/v1"))

	if got := llm.ContextWindow("omlx-status-model"); got != 131072 {
		t.Fatalf("want 131072, got %d", got)
	}
}

// ollama keeps winning via /api/show num_ctx, which beats the model's
// theoretical maximum.
func TestPrewarmLocalContext_OllamaShowWins(t *testing.T) {
	resetCtxProbeState(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/show" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"parameters":"num_ctx 8192\n","model_info":{"llama.context_length":131072}}`))
	}))
	defer srv.Close()

	prewarmLocalContext(ctxProbeCfg("ollama", "ollama-probe-model:7b", srv.URL+"/v1"))

	if got := llm.ContextWindow("ollama-probe-model:7b"); got != 8192 {
		t.Fatalf("want 8192, got %d", got)
	}
}

func TestPrewarmLocalContext_NoBaseURLNoop(t *testing.T) {
	resetCtxProbeState(t)
	prewarmLocalContext(ctxProbeCfg("omlx", "noop-model", ""))
	if got := llm.ContextWindow("noop-model"); got != 0 {
		t.Fatalf("want 0, got %d", got)
	}
}
