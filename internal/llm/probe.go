package llm

import (
	"cmp"
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/elhenro/bee/internal/config"
)

// probeHTTPClient is the http client used by ProbeContextLength. Keep the
// timeout short — probe is best-effort; a slow local server should not stall
// the first turn. Overridable from tests via SetProbeHTTPClient.
var probeHTTPClient = &http.Client{Timeout: 3 * time.Second}

// SetProbeHTTPClient swaps the package probe client. Tests route through
// httptest servers.
func SetProbeHTTPClient(c *http.Client) { probeHTTPClient = c }

// probedKey is keyed by provider-name+model-id so probe-once dedupe holds
// across multiple Engine instances in the same process (swarm/fan/hive).
var (
	probed   = map[string]bool{}
	probedMu sync.Mutex
)

func probeKey(name, modelID string) string { return name + "\x00" + modelID }

func markProbed(name, modelID string) {
	probedMu.Lock()
	defer probedMu.Unlock()
	probed[probeKey(name, modelID)] = true
}

func wasProbed(name, modelID string) bool {
	probedMu.Lock()
	defer probedMu.Unlock()
	return probed[probeKey(name, modelID)]
}

// ResetProbed drops dedupe state. Tests call between cases.
func ResetProbed() {
	probedMu.Lock()
	defer probedMu.Unlock()
	probed = map[string]bool{}
}

// ProbeContextLength returns the live context window for modelID, learning
// it from the provider if not already in the cache. Order:
//  1. ContextWindow cache (live + hardcoded) — short-circuits.
//  2. Ollama native /api/show — POST {"name": modelID}, parse model_info.
//  3. OpenAI-compat /v1/models — populates cache for any model the server
//     advertises a context_length for (openrouter, some lm-studio versions).
//
// Returns the learned value (also stored via RememberContextLength) or 0
// when nothing could be determined. Safe to call concurrently and repeatedly
// — dedupes per (provider,model).
func ProbeContextLength(ctx context.Context, name string, cfg config.ProviderConfig, modelID string) int {
	if cw := ContextWindow(modelID); cw > 0 {
		return cw
	}
	if wasProbed(name, modelID) {
		return 0
	}
	markProbed(name, modelID)

	// Anthropic/responses/gemini: no usable model-metadata endpoint we can
	// probe generically. Skip — the hardcoded table covers these families.
	if isAnthropicWire(cfg.WireAPI) || cfg.WireAPI == "responses" || cfg.WireAPI == "gemini" {
		return 0
	}

	if isOllamaProvider(name, cfg) {
		if n := probeOllamaShow(ctx, cfg.BaseURL, modelID); n > 0 {
			RememberContextLength(modelID, n)
			return n
		}
	}

	// Generic openai-compat /v1/models. Best-effort: ollama returns no
	// context_length here but openrouter / some lm-studio builds do. The
	// per-model cache populated by storeCache is what we read back via
	// ContextWindow below.
	if strings.TrimSpace(cfg.BaseURL) != "" {
		_, _ = ListModels(ctx, name, cfg)
		if cw := ContextWindow(modelID); cw > 0 {
			return cw
		}
	}
	return 0
}

// isOllamaProvider reports whether name+cfg looks like ollama. Matches the
// provider name plus the canonical localhost:11434 base url so a renamed
// provider block still hits the probe.
func isOllamaProvider(name string, cfg config.ProviderConfig) bool {
	if strings.EqualFold(name, "ollama") {
		return true
	}
	return strings.Contains(strings.ToLower(cfg.BaseURL), "11434")
}

// probeOllamaShow reads the architecture context length from the native show
// endpoint's model_info. baseURL is the chat-completions base
// (`http://host:11434/v1`). 0 on any failure.
func probeOllamaShow(ctx context.Context, baseURL, modelID string) int {
	root := cmp.Or(OllamaBaseURL(baseURL), "http://localhost:11434")
	show, err := ollamaShow(ctx, probeHTTPClient, root, modelID)
	if err != nil {
		return 0
	}
	return contextLengthFromModelInfo(show.ModelInfo)
}
