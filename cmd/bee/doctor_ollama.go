// Ollama-specific probes for `bee doctor`. Daemon liveness, model
// availability, num_ctx cache warm-up. Only runs when ollama is the active
// provider.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// ollamaModel is one /api/tags entry.
type ollamaModel struct {
	Name  string `json:"name"`
	Model string `json:"model"`
}

// checkOllama only runs when ollama is the active provider. Daemon down +
// model-not-pulled are WARN — bee should still work with other providers
// configured. A successful tags fetch also probes /api/show so we surface
// the real num_ctx (rather than the misleading fallback).
func checkOllama(cfg config.Config) []check {
	if cfg.DefaultProvider != "ollama" {
		return nil
	}
	res := func(status, detail, remedy string) []check {
		return []check{{Name: "ollama", Status: status, Detail: detail, Remedy: remedy}}
	}
	p, ok := cfg.Providers["ollama"]
	if !ok {
		return res("warn", "provider config missing", "")
	}

	base := llm.OllamaBaseURL(p.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/tags", nil)
	if err != nil {
		return res("warn", "build request: "+err.Error(), "")
	}
	resp, err := doctorHTTPClient.Do(req)
	if err != nil {
		return res("warn", "daemon not responding at "+base, "start ollama (`ollama serve`) or remove ollama as default provider")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return res("warn", fmt.Sprintf("daemon returned %d at %s/api/tags", resp.StatusCode, base), "")
	}

	var tags struct {
		Models []ollamaModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return res("warn", "decode tags: "+err.Error(), "")
	}

	pulled := slices.ContainsFunc(tags.Models, func(m ollamaModel) bool {
		return m.Name == cfg.DefaultModel || m.Model == cfg.DefaultModel
	})
	if !pulled {
		return res("warn", fmt.Sprintf("model %s not pulled", cfg.DefaultModel), "ollama pull "+cfg.DefaultModel)
	}

	// model present — probe num_ctx for the OK detail and warm the cache so
	// the loop's contextBudget reflects reality from the first turn.
	pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer pcancel()
	n, _ := llm.ProbeOllamaContext(pctx, doctorHTTPClient, p.BaseURL, cfg.DefaultModel)
	if n > 0 {
		llm.RememberContextLength(cfg.DefaultModel, n)
		return res("ok", fmt.Sprintf("model pulled, num_ctx=%d", n), "")
	}
	return res("ok", "model pulled (num_ctx unknown)", "")
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
	ollama := llm.OllamaBaseURL(localProviderBase(cfg, "ollama", "http://localhost:11434/v1"))
	if names, ok := probeModelNames(ollama + "/api/tags"); ok {
		out = append(out, localServerInfo("ollama", ollama, names))
	}
	omlx := localProviderBase(cfg, "omlx", "http://localhost:8000/v1")
	if names, ok := probeModelNames(strings.TrimRight(omlx, "/") + "/models"); ok {
		out = append(out, localServerInfo("omlx", omlx, names))
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

// probeModelNames does a quick GET and returns the model names the server
// lists, in either the ollama (models[].name) or OpenAI-compatible (data[].id)
// shape. ok=false on any failure (server down) so doctor stays quiet.
func probeModelNames(url string) ([]string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false
	}
	resp, err := doctorHTTPClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, false
	}
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil {
		return nil, false
	}
	var names []string
	for _, m := range body.Models {
		names = append(names, m.Name)
	}
	for _, m := range body.Data {
		names = append(names, m.ID)
	}
	return names, true
}

// localServerInfo is the info check for a running local server.
func localServerInfo(name, base string, names []string) check {
	return check{
		Name:   name,
		Status: "info",
		Detail: fmt.Sprintf("running at %s with %d model(s)%s — set default_provider=%s for zero-config local", base, len(names), sampleNames(names), name),
	}
}

// sampleNames renders up to the first three model names as " (a, b, c)" for
// the info detail, or "" when the list is empty.
func sampleNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return " (" + strings.Join(names[:min(len(names), 3)], ", ") + ")"
}
