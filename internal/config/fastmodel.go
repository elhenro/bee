package config

import "strings"

// FastModelOf resolves the cheap side-eval model: cfg.FastModel when set,
// otherwise the default model.
func FastModelOf(c Config) string {
	if strings.TrimSpace(c.FastModel) != "" {
		return c.FastModel
	}
	return c.DefaultModel
}
