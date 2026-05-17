package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/elhenro/bee/internal/auth"
	"github.com/elhenro/bee/internal/config"
)

// Model is a single entry in a provider's catalogue.
type Model struct {
	ID            string
	Name          string
	ContextLength int
	Pricing       string
}

// modelCacheTTL is how long a successful /models response stays warm before
// ListModels re-fetches. Picker UI calls this on every open, so a short TTL
// keeps it snappy without hammering upstream.
const modelCacheTTL = 10 * time.Minute

// modelsHTTPTimeout caps the /models GET. provider lists are tiny, so 8s is
// plenty even for high-latency networks.
const modelsHTTPTimeout = 8 * time.Second

type cacheEntry struct {
	models []Model
	at     time.Time
}

var (
	modelCache   = map[string]cacheEntry{}
	modelCacheMu sync.Mutex
	// modelsHTTPClient is overridable from tests via SetModelsHTTPClient.
	modelsHTTPClient = &http.Client{Timeout: modelsHTTPTimeout}
)

// SetModelsHTTPClient replaces the package HTTP client. Tests use this to
// route through an httptest server with a stricter timeout.
func SetModelsHTTPClient(c *http.Client) { modelsHTTPClient = c }

// isAnthropicWire reports whether the wire-api string belongs to the Anthropic
// family. Used to gate the hardcoded model list and provider dispatch.
func isAnthropicWire(w string) bool {
	switch w {
	case "anthropic", "anthropic-messages":
		return true
	}
	return false
}

// resolveProviderKey mirrors config.resolveAPIKey for stateless callers like
// the model-list fetcher: env var first, then stored ~/.bee/auth/<name>.key.
// Returns "" when no key is available (callers send no Authorization header).
func resolveProviderKey(name string, cfg config.ProviderConfig) string {
	if cfg.EnvKey != "" {
		if key := os.Getenv(cfg.EnvKey); key != "" {
			return key
		}
	}
	if name == "" {
		return ""
	}
	dir, err := auth.DefaultDir()
	if err != nil {
		return ""
	}
	key, _ := auth.LoadAPIKey(dir, name)
	return key
}

// ClearModelCache drops every cached entry. Tests call this between cases.
func ClearModelCache() {
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()
	modelCache = map[string]cacheEntry{}
}

// ListModels returns the model catalogue for the given provider. For
// anthropic-wire providers or empty base URLs we return a curated hardcoded
// list — Anthropic's /models endpoint is gated and not all wire-compat
// servers expose one. Results are cached per-name with a 10-minute TTL.
func ListModels(ctx context.Context, name string, cfg config.ProviderConfig) ([]Model, error) {
	if cached, ok := lookupCache(name); ok {
		return cached, nil
	}

	if isAnthropicWire(cfg.WireAPI) || strings.TrimSpace(cfg.BaseURL) == "" {
		key := cfg.WireAPI
		if isAnthropicWire(key) {
			key = "anthropic"
		}
		models := hardcodedModels(key)
		storeCache(name, models)
		return models, nil
	}
	// Responses-wire backends (chatgpt subscription) don't expose /models —
	// the codex backend only serves /responses. Skip the GET that would
	// 401 with no token and use the curated list for `name` instead.
	if cfg.WireAPI == "responses" {
		models := hardcodedFallback(name)
		if models == nil {
			models = []Model{}
		}
		storeCache(name, models)
		return models, nil
	}

	models, err := fetchModels(ctx, name, cfg)
	if err != nil {
		// fallback: if upstream rejects the call but the provider name maps
		// to a known wire family, still surface a usable list.
		if fb := hardcodedFallback(name); fb != nil {
			storeCache(name, fb)
			return fb, nil
		}
		return nil, err
	}
	enrichContextLengths(ctx, name, cfg, models)
	storeCache(name, models)
	return models, nil
}

// enrichContextLengths fills missing ContextLength values by probing the
// non-standard /models/status endpoint. omlx 0.3.x reports per-model
// max_context_window there even though plain /v1/models follows the stock
// OpenAI shape (no context_length field). Errors and non-200s are swallowed
// — this is best-effort enrichment, not a hard requirement.
func enrichContextLengths(ctx context.Context, name string, cfg config.ProviderConfig, models []Model) {
	needs := false
	for _, m := range models {
		if m.ContextLength == 0 {
			needs = true
			break
		}
	}
	if !needs || strings.TrimSpace(cfg.BaseURL) == "" {
		return
	}
	url := strings.TrimRight(cfg.BaseURL, "/") + "/models/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/json")
	if key := resolveProviderKey(name, cfg); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := modelsHTTPClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return
	}
	var body struct {
		Models []struct {
			ID               string `json:"id"`
			MaxContextWindow int    `json:"max_context_window"`
			MaxTokens        int    `json:"max_tokens"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return
	}
	idx := map[string]int{}
	for _, s := range body.Models {
		n := s.MaxContextWindow
		if n == 0 {
			n = s.MaxTokens
		}
		if n > 0 {
			idx[s.ID] = n
		}
	}
	if len(idx) == 0 {
		return
	}
	for i := range models {
		if models[i].ContextLength > 0 {
			continue
		}
		if n, ok := idx[models[i].ID]; ok {
			models[i].ContextLength = n
		}
	}
}

func lookupCache(name string) ([]Model, bool) {
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()
	e, ok := modelCache[name]
	if !ok {
		return nil, false
	}
	if time.Since(e.at) > modelCacheTTL {
		delete(modelCache, name)
		return nil, false
	}
	// return a copy so callers can't mutate the cached slice
	out := make([]Model, len(e.models))
	copy(out, e.models)
	return out, true
}

func storeCache(name string, models []Model) {
	modelCacheMu.Lock()
	cp := make([]Model, len(models))
	copy(cp, models)
	modelCache[name] = cacheEntry{models: cp, at: time.Now()}
	modelCacheMu.Unlock()
	// seed the package-wide live ctx cache so ContextWindow can answer for
	// any model the API surfaced — not just the curated hardcoded table.
	for _, m := range models {
		if m.ContextLength > 0 {
			RememberContextLength(m.ID, m.ContextLength)
		}
	}
}

// fetchModels GETs <base_url>/models. Auth header mirrors openai_compat:
// env var (cfg.EnvKey) first, then ~/.bee/auth/<name>.key (enrolled via
// /login). Same resolution order config.Load uses for the runtime APIKey.
func fetchModels(ctx context.Context, name string, cfg config.ProviderConfig) ([]Model, error) {
	url := strings.TrimRight(cfg.BaseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if key := resolveProviderKey(name, cfg); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := modelsHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw))
	}

	var body struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			ContextLength int    `json:"context_length"`
			Pricing       any    `json:"pricing"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	models := make([]Model, 0, len(body.Data))
	for _, m := range body.Data {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		models = append(models, Model{
			ID:            m.ID,
			Name:          name,
			ContextLength: m.ContextLength,
			Pricing:       formatPricing(m.Pricing),
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

// formatPricing collapses openrouter-style {prompt,completion} objects into
// a short string. anything we don't recognise becomes empty.
func formatPricing(p any) string {
	m, ok := p.(map[string]any)
	if !ok {
		return ""
	}
	in, _ := m["prompt"].(string)
	out, _ := m["completion"].(string)
	if in == "" && out == "" {
		return ""
	}
	return fmt.Sprintf("in:%s out:%s", in, out)
}

// hardcodedModels + hardcodedFallback live in models_hardcoded.go.
