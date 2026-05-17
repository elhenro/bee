package config

import "strings"

// ActiveProfile returns the profile named by c.Profile, falling back to
// "normal" and then to a zero Profile if neither exists. "auto" resolves
// to tiny/normal/large by provider + model class via ResolveAutoProfileForProvider.
func ActiveProfile(c Config) Profile {
	name := c.Profile
	if name == "auto" {
		name = ResolveAutoProfileForProvider(c.DefaultProvider, c.DefaultModel)
	}
	if p, ok := c.Profiles[name]; ok {
		return p
	}
	if p, ok := c.Profiles["normal"]; ok {
		return p
	}
	return Profile{}
}

// ResolveAutoProfile picks a profile name from a model id alone. Kept for
// back-compat; prefer ResolveAutoProfileForProvider so local providers
// (ollama / lmstudio) get the tiny surface regardless of model size.
func ResolveAutoProfile(model string) string {
	return ResolveAutoProfileForProvider("", model)
}

// ResolveAutoProfileForProvider picks a profile name from provider + model.
// Local providers (ollama, lmstudio) always resolve to tiny — even a 35b
// local model fumbles a wide tool surface and benefits from plain-english
// prompts. Heuristic, not exhaustive; falls back to "normal".
//
// Why: pi-coding-agent works well with the same local models because it
// advertises 4 plain-named tools (read/bash/edit/write) in normal english.
// Mirror that for local runs while keeping richer surfaces for frontier
// hosted models.
func ResolveAutoProfileForProvider(provider, model string) string {
	if IsLocalProvider(provider) {
		return "tiny"
	}
	if model == "" {
		return "normal"
	}
	m := strings.ToLower(model)
	// strip provider prefix ("openrouter/...", "deepseek/...") so the
	// suffix carries the actual model id.
	if idx := strings.LastIndex(m, "/"); idx >= 0 {
		m = m[idx+1:]
	}
	switch {
	case containsAny(m, "flash", "mini", "nano", "haiku", "8b", "7b", "3b", "1b", "phi"):
		return "tiny"
	case containsAny(m, "opus", "sonnet-4-7", "sonnet-4-6"):
		return "large"
	default:
		return "normal"
	}
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// ApplyProfile overlays the active profile onto top-level Config fields
// that are still zero. Explicit user choices win over profile defaults;
// the profile only fills gaps. Also resolves c.Profile == "auto" in place
// so downstream code sees the concrete name. Local providers (ollama /
// lmstudio) skip the auto-mode classifier — c.Mode falls back to "edit"
// because the classifier round-trip is wasteful on slow on-host models.
func ApplyProfile(c Config) Config {
	if c.Profile == "auto" {
		c.Profile = ResolveAutoProfileForProvider(c.DefaultProvider, c.DefaultModel)
	}
	p := ActiveProfile(c)
	// "" or "auto" → take from the active profile. Any explicit user choice
	// (full/lite/ultra/off) wins.
	if c.Caveman == "" || c.Caveman == "auto" {
		c.Caveman = p.Caveman
	}
	if c.Mode == "auto" && IsLocalProvider(c.DefaultProvider) {
		c.Mode = "edit"
	}
	return c
}
