package llm

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/config"
)

// enrichSizes fills Model.SizeBytes for on-host servers that report it, so the
// picker can show how much memory a model needs. Weights dominate that
// footprint, so the reported weight size stands in for it. Best-effort: a
// server without the endpoint leaves SizeBytes zero.
func enrichSizes(ctx context.Context, name string, cfg config.ProviderConfig, models []Model) {
	if !config.IsLocalProvider(name) || strings.TrimSpace(cfg.BaseURL) == "" {
		return
	}
	var sizes map[string]int64
	switch strings.ToLower(name) {
	case "omlx":
		sizes = omlxSizes(ctx, name, cfg)
	case "ollama":
		sizes = ollamaSizes(ctx, cfg)
	}
	for i := range models {
		models[i].SizeBytes = sizes[models[i].ID]
	}
}

// omlxSizes reads per-model weight bytes from the non-standard /models/status
// endpoint: actual_size once a model is loaded, estimated_size before that.
func omlxSizes(ctx context.Context, name string, cfg config.ProviderConfig) map[string]int64 {
	var body struct {
		Models []struct {
			ID            string `json:"id"`
			EstimatedSize int64  `json:"estimated_size"`
			ActualSize    int64  `json:"actual_size"`
		} `json:"models"`
	}
	if getJSON(ctx, strings.TrimRight(cfg.BaseURL, "/")+"/models/status", bearerHeader(name, cfg), &body) != nil {
		return nil
	}
	out := make(map[string]int64, len(body.Models))
	for _, s := range body.Models {
		out[s.ID] = max(s.ActualSize, s.EstimatedSize)
	}
	return out
}

// ollamaSizes reads per-model blob bytes from the native /api/tags listing.
func ollamaSizes(ctx context.Context, cfg config.ProviderConfig) map[string]int64 {
	var body struct {
		Models []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"models"`
	}
	if getJSON(ctx, OllamaBaseURL(cfg.BaseURL)+"/api/tags", nil, &body) != nil {
		return nil
	}
	out := make(map[string]int64, len(body.Models))
	for _, s := range body.Models {
		out[s.Name] = s.Size
	}
	return out
}
