package main

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"

	"github.com/elhenro/bee/internal/config"
)

// persistVisionModel records the user's answer to "does this model accept
// images?" as [vision.models] "<model>" = <bool> in config.toml, keeping every
// other key. Missing file is created.
func persistVisionModel(model string, hasVision bool) error {
	if model == "" {
		return nil
	}
	path := config.ConfigPath()
	root := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	}
	vis, _ := root["vision"].(map[string]any)
	if vis == nil {
		vis = map[string]any{}
	}
	models, _ := vis["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}
	models[model] = hasVision
	vis["models"] = models
	root["vision"] = vis
	out, err := toml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal toml: %w", err)
	}
	return atomicWriteApproval(path, out)
}
