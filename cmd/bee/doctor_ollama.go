// Ollama-specific probes for `bee doctor`. Daemon liveness, model
// availability, num_ctx cache warm-up. Only runs when ollama is the active
// provider.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
