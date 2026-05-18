package loop

import "github.com/elhenro/bee/internal/llm"

// profileToolAllowlist trims the tool surface advertised per profile. Registry
// stays full so any explicit call still executes, but the manifest + request
// schema only carry the listed tools.
//
//   - tiny: 4-tool minimum for 4k-ctx models. bash covers grep/find for them.
//   - normal: search/glob/ls/edit/write/bash/read plus the knowledge tools.
//   - large: full surface incl. apply_patch + hashline_edit for capable models
//     (no entry below; missing profile = pass-through).
//
// A profile absent from this map passes through unfiltered.
var profileToolAllowlist = map[string]map[string]bool{
	"tiny": {
		"bash":  true,
		"read":  true,
		"write": true,
		"edit":  true,
	},
	"normal": {
		"bash":             true,
		"read":             true,
		"write":            true,
		"edit":             true,
		"search":           true,
		"glob":             true,
		"ls":               true,
		"knowledge_search": true,
		"knowledge_write":  true,
	},
}

// filterToolSpecsDisabled removes any spec whose name appears in disabled.
// Live filter so toggling /tools mid-session takes effect on the next turn
// without rebuilding the registry.
func filterToolSpecsDisabled(specs []llm.ToolSpec, disabled []string) []llm.ToolSpec {
	if len(disabled) == 0 {
		return specs
	}
	drop := make(map[string]bool, len(disabled))
	for _, n := range disabled {
		drop[n] = true
	}
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, s := range specs {
		if drop[s.Name] {
			continue
		}
		out = append(out, s)
	}
	return out
}

// filterToolSpecsForProfile drops tool specs that fall outside the profile's
// allowlist. Profiles with no allowlist (e.g. "large") pass through. Names
// in extras are force-allowed regardless of profile — the opt-in hatch for
// power tools like apply_patch / hashline_edit when staying on a small
// profile.
func filterToolSpecsForProfile(specs []llm.ToolSpec, profile string, extras ...string) []llm.ToolSpec {
	allow, ok := profileToolAllowlist[profile]
	if !ok {
		return specs
	}
	extra := make(map[string]bool, len(extras))
	for _, n := range extras {
		extra[n] = true
	}
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, s := range specs {
		if allow[s.Name] || extra[s.Name] {
			out = append(out, s)
		}
	}
	return out
}
