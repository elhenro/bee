package loop

import (
	"slices"

	"github.com/elhenro/bee/internal/llm"
)

// keepSpecs returns the specs whose name passes keep, as a fresh slice.
func keepSpecs(specs []llm.ToolSpec, keep func(name string) bool) []llm.ToolSpec {
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, s := range specs {
		if keep(s.Name) {
			out = append(out, s)
		}
	}
	return out
}

// filterToolSpecsDisabled removes any spec whose name appears in disabled.
// Live filter so toggling /tools mid-session takes effect on the next turn
// without rebuilding the registry.
func filterToolSpecsDisabled(specs []llm.ToolSpec, disabled []string) []llm.ToolSpec {
	if len(disabled) == 0 {
		return specs
	}
	return keepSpecs(specs, func(n string) bool { return !slices.Contains(disabled, n) })
}
