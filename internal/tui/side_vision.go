package tui

import (
	"errors"
	"fmt"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
)

// VisionFallback configures the session's fallback vision model (no persist).
// Empty model reports current status. Used when the main model can't see images.
func (s *tuiSide) VisionFallback(model, endpoint, api string) (string, error) {
	if s.m == nil || s.m.eng == nil {
		return "", errors.New("no tui state")
	}
	cfg := &s.m.eng.Cfg
	if model == "" {
		main := cfg.DefaultModel
		if llm.SupportsVision(main) {
			return fmt.Sprintf("main model %q has vision — no fallback needed", main), nil
		}
		if cfg.Vision.Model == "" {
			return fmt.Sprintf("main model %q has no vision, no fallback set. usage: /vision <model> [endpoint] [api]", main), nil
		}
		return fmt.Sprintf("vision fallback: %s @ %s (api=%s)", cfg.Vision.Model, visionEndpoint(cfg), visionAPI(cfg.Vision.API)), nil
	}
	cfg.Vision.Model = model
	if endpoint != "" {
		cfg.Vision.Endpoint = endpoint
	}
	if api != "" {
		cfg.Vision.API = api
	}
	if visionEndpoint(cfg) == "" {
		return "", fmt.Errorf("vision: endpoint required. usage: /vision %s <endpoint> [api]", model)
	}
	return fmt.Sprintf("vision fallback set: %s @ %s (api=%s). add [vision] to config.toml to persist.",
		cfg.Vision.Model, visionEndpoint(cfg), visionAPI(cfg.Vision.API)), nil
}

func visionAPI(a string) string {
	if a == "" {
		return "openai"
	}
	return a
}

// visionEndpoint resolves the effective endpoint, inheriting from the named
// provider's base_url when [vision] endpoint is unset.
func visionEndpoint(cfg *config.Config) string {
	v := cfg.Vision
	if v.Endpoint != "" {
		return v.Endpoint
	}
	if v.Provider != "" {
		if pc, ok := cfg.Providers[v.Provider]; ok {
			return pc.BaseURL
		}
	}
	return ""
}
