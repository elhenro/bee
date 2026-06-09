// Ollama-specific probes for `bee doctor`. Daemon liveness, model
// availability, num_ctx cache warm-up. Only runs when ollama is the active
// provider.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// checkOllama only runs when ollama is the active provider. Daemon down +
// model-not-pulled are WARN — bee should still work with other providers
// configured. A successful tags fetch also probes /api/show so we surface
// the real num_ctx (rather than the misleading fallback).
func checkOllama(cfg config.Config) []check {
	if cfg.DefaultProvider != "ollama" {
		return nil
	}
	p, ok := cfg.Providers["ollama"]
	if !ok {
		return []check{{Name: "ollama", Status: "warn", Detail: "provider config missing"}}
	}

	base := llm.OllamaBaseURL(p.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return []check{{Name: "ollama", Status: "warn", Detail: "build request: " + err.Error()}}
	}
	resp, err := doctorHTTPClient.Do(req)
	if err != nil {
		return []check{{
			Name:   "ollama",
			Status: "warn",
			Detail: "daemon not responding at " + base,
			Remedy: "start ollama (`ollama serve`) or remove ollama as default provider",
		}}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return []check{{
			Name:   "ollama",
			Status: "warn",
			Detail: fmt.Sprintf("daemon returned %d at %s/api/tags", resp.StatusCode, base),
		}}
	}

	var tags struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return []check{{Name: "ollama", Status: "warn", Detail: "decode tags: " + err.Error()}}
	}

	if !hasOllamaModel(tags.Models, cfg.DefaultModel) {
		return []check{{
			Name:   "ollama",
			Status: "warn",
			Detail: fmt.Sprintf("model %s not pulled", cfg.DefaultModel),
			Remedy: "ollama pull " + cfg.DefaultModel,
		}}
	}

	// model present — probe num_ctx for the OK detail and warm the cache so
	// the loop's contextBudget reflects reality from the first turn.
	pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer pcancel()
	n, _ := llm.ProbeOllamaContext(pctx, doctorHTTPClient, p.BaseURL, cfg.DefaultModel)
	if n > 0 {
		llm.RememberContextLength(cfg.DefaultModel, n)
		return []check{{
			Name:   "ollama",
			Status: "ok",
			Detail: fmt.Sprintf("model pulled, num_ctx=%d", n),
		}}
	}
	return []check{{
		Name:   "ollama",
		Status: "ok",
		Detail: "model pulled (num_ctx unknown)",
	}}
}

// checkLocalServers surfaces a running local inference server (ollama / omlx)
// as info even when it isn't the active provider, so a default-config user
// (openrouter) learns a free local backend is available. Info-level only —
// never warn/fail. Skipped when a local provider is already the default, since
// checkOllama reports that case in full.
func checkLocalServers(cfg config.Config) []check {
	if config.IsLocalProvider(cfg.DefaultProvider) {
		return nil
	}
	var out []check
	if c, ok := probeOllamaInfo(cfg); ok {
		out = append(out, c)
	}
	if c, ok := probeOpenAIModelsInfo("omlx", localProviderBase(cfg, "omlx", "http://localhost:8000/v1")); ok {
		out = append(out, c)
	}
	return out
}

// localProviderBase returns the configured base URL for a provider, falling
// back to def when the provider isn't configured.
func localProviderBase(cfg config.Config, name, def string) string {
	if p, ok := cfg.Providers[name]; ok && p.BaseURL != "" {
		return p.BaseURL
	}
	return def
}

// probeOllamaInfo does a quick /api/tags fetch and reports an info check when
// ollama answers. ok=false on any failure (server down) so doctor stays quiet.
func probeOllamaInfo(cfg config.Config) (check, bool) {
	base := llm.OllamaBaseURL(localProviderBase(cfg, "ollama", "http://localhost:11434/v1"))
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return check{}, false
	}
	resp, err := doctorHTTPClient.Do(req)
	if err != nil {
		return check{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return check{}, false
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return check{}, false
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return check{
		Name:   "ollama",
		Status: "info",
		Detail: fmt.Sprintf("running at %s with %d model(s)%s — set default_provider=ollama for zero-config local", base, len(names), sampleNames(names)),
	}, true
}

// probeOpenAIModelsInfo does a quick /models fetch (OpenAI-compatible) and
// reports an info check when the server answers.
func probeOpenAIModelsInfo(name, base string) (check, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/models", nil)
	if err != nil {
		return check{}, false
	}
	resp, err := doctorHTTPClient.Do(req)
	if err != nil {
		return check{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return check{}, false
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return check{}, false
	}
	names := make([]string, 0, len(body.Data))
	for _, m := range body.Data {
		names = append(names, m.ID)
	}
	return check{
		Name:   name,
		Status: "info",
		Detail: fmt.Sprintf("running at %s with %d model(s)%s — set default_provider=%s for zero-config local", base, len(names), sampleNames(names), name),
	}, true
}

// sampleNames renders up to the first three model names as " (a, b, c)" for
// the info detail, or "" when the list is empty.
func sampleNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	n := names
	if len(n) > 3 {
		n = n[:3]
	}
	return " (" + strings.Join(n, ", ") + ")"
}

func hasOllamaModel(models []struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}, want string) bool {
	for _, m := range models {
		if m.Name == want || m.Model == want {
			return true
		}
	}
	return false
}
