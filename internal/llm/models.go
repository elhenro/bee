package llm

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
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
	// SizeBytes is the weight footprint an on-host server reports for the
	// model. 0 when unknown (hosted providers, servers without the endpoint).
	SizeBytes int64
	// InputModalities is the catalogue's declared input types ("text",
	// "image", ...). nil when the provider doesn't say.
	InputModalities []string
}

// modelsHTTPTimeout caps the /models GET. provider lists are tiny, so 8s is
// plenty even for high-latency networks.
const modelsHTTPTimeout = 8 * time.Second

// modelsHTTPClient is overridable from tests via SetModelsHTTPClient.
var modelsHTTPClient = &http.Client{Timeout: modelsHTTPTimeout}

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

// bearerHeader is the Authorization header for the provider's key, nil when it
// has none.
func bearerHeader(name string, cfg config.ProviderConfig) map[string]string {
	if key := resolveProviderKey(name, cfg); key != "" {
		return map[string]string{"Authorization": "Bearer " + key}
	}
	return nil
}

// getJSON GETs url with the models client and decodes the JSON body into v. A
// status >= 400 is an error carrying the start of the body.
func getJSON(ctx context.Context, url string, headers map[string]string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	resp, err := modelsHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("status %d: %s", resp.StatusCode, raw)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(v); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// ListModels returns the model catalogue for the given provider. The
// anthropic wire is short-circuited only for the real Anthropic provider —
// third parties that copy the wire shape (MiniMax and friends) usually DO
// expose a /models endpoint and we should query it. Empty base URLs fall
// back to a curated list. Results are cached per-name with a 10-minute TTL.
func ListModels(ctx context.Context, name string, cfg config.ProviderConfig) ([]Model, error) {
	if cached, ok := lookupCache(name); ok {
		return cached, nil
	}
	models, err := loadCatalogue(ctx, name, cfg)
	if err != nil {
		return nil, err
	}
	storeCache(name, models)
	return models, nil
}

// fallbackOrEmpty is the curated list for name, never nil.
func fallbackOrEmpty(name string) []Model {
	if fb := hardcodedFallback(name); fb != nil {
		return fb
	}
	return []Model{}
}

func loadCatalogue(ctx context.Context, name string, cfg config.ProviderConfig) ([]Model, error) {
	if isAnthropicWire(cfg.WireAPI) && name == "anthropic" {
		return anthropicModels(), nil
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return fallbackOrEmpty(name), nil
	}
	// Responses-wire backends. chatgpt.com exposes a plan-filtered list at
	// /backend-api/codex/models?client_version=X — query it live so users see
	// exactly what their account can call. Other responses providers don't
	// have a /models endpoint, so fall back to the curated list.
	if cfg.WireAPI == "responses" {
		if name == "chatgpt" {
			acct := ""
			if cfg.OAuth != nil {
				acct = cfg.OAuth.AccountIDHeader
			}
			if live, err := FetchChatGPTModels(ctx, cfg.BaseURL, acct); err == nil && len(live) > 0 {
				return live, nil
			}
		}
		return fallbackOrEmpty(name), nil
	}

	models, err := fetchModels(ctx, name, cfg)
	if err != nil {
		// fallback: if upstream rejects the call but the provider name maps
		// to a known wire family, still surface a usable list.
		if fb := hardcodedFallback(name); fb != nil {
			return fb, nil
		}
		return nil, err
	}
	enrichContextLengths(ctx, name, cfg, models)
	enrichSizes(ctx, name, cfg, models)
	return models, nil
}

// enrichContextLengths fills missing ContextLength values by probing the
// non-standard /models/status endpoint. omlx 0.3.x reports per-model
// max_context_window there even though plain /v1/models follows the stock
// chat-completions shape (no context_length field). Errors are swallowed;
// this is best-effort enrichment, not a hard requirement.
func enrichContextLengths(ctx context.Context, name string, cfg config.ProviderConfig, models []Model) {
	missing := slices.ContainsFunc(models, func(m Model) bool { return m.ContextLength == 0 })
	if !missing || strings.TrimSpace(cfg.BaseURL) == "" {
		return
	}
	var body struct {
		Models []struct {
			ID               string `json:"id"`
			MaxContextWindow int    `json:"max_context_window"`
			MaxTokens        int    `json:"max_tokens"`
		} `json:"models"`
	}
	if getJSON(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/models/status", bearerHeader(name, cfg), &body) != nil {
		return
	}
	window := map[string]int{}
	for _, s := range body.Models {
		if n := cmp.Or(s.MaxContextWindow, s.MaxTokens); n > 0 {
			window[s.ID] = n
		}
	}
	for i := range models {
		if models[i].ContextLength == 0 {
			models[i].ContextLength = window[models[i].ID]
		}
	}
}

// fetchModels GETs <base_url>/models. Auth header mirrors openai_compat:
// env var (cfg.EnvKey) first, then ~/.bee/auth/<name>.key (enrolled via
// /login). Same resolution order config.Load uses for the runtime APIKey.
func fetchModels(ctx context.Context, name string, cfg config.ProviderConfig) ([]Model, error) {
	var body struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			// DisplayName is the anthropic-compat shape some vendors use
			// (MiniMax returns `display_name` alongside `id`). Falls back to
			// the ID when both `name` and `display_name` are missing.
			DisplayName   string `json:"display_name"`
			ContextLength int    `json:"context_length"`
			MaxModelLen   int    `json:"max_model_len"`
			Pricing       any    `json:"pricing"`
			Architecture  struct {
				InputModalities []string `json:"input_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if err := getJSON(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/models", bearerHeader(name, cfg), &body); err != nil {
		return nil, err
	}

	models := make([]Model, 0, len(body.Data))
	for _, m := range body.Data {
		models = append(models, Model{
			ID:   m.ID,
			Name: cmp.Or(m.Name, m.DisplayName, m.ID),
			// omlx advertises the window as max_model_len on the stock /v1/models
			// shape (no context_length field). Fall back to it so detection works
			// without the secondary /models/status probe.
			ContextLength:   cmp.Or(m.ContextLength, m.MaxModelLen),
			Pricing:         formatPricing(m.Pricing),
			InputModalities: m.Architecture.InputModalities,
		})
	}
	slices.SortFunc(models, func(a, b Model) int { return cmp.Compare(a.ID, b.ID) })
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

// hardcodedFallback + anthropicModels live in models_hardcoded.go.
