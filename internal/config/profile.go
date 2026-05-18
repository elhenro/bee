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
// Why: small local models work best with a narrow, plain-named tool set
// (read/bash/edit/write) described in normal english. Local runs get
// that surface; richer surfaces stay for frontier hosted models.
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
	// hyphen/underscore/dot/colon/slash/start/end are the natural token edges
	// in model ids (e.g. `llama-3.1-8b-instruct`, `gemini-2.5-flash-lite`).
	// Token-boundary matches stop `8b` from hitting `mistral-87b` and `phi`
	// from hitting hypothetical `alphi-…` builds.
	switch {
	// sparse MoE: 3B-active class (qwen3-a3b, granite-a7b, mixtral-style).
	// Reasoning depth stays small-model even at 35B/128k. `a3b` etc are
	// substring-matched because they only appear inside size descriptors.
	case containsAny(m, "a3b", "a7b", "-moe", "moe-", "coder-30b"):
		return "tiny"
	case containsToken(m, "flash", "mini", "nano", "haiku", "phi") ||
		hasSizeToken(m, "1b", "3b", "7b", "8b"):
		return "tiny"
	case containsToken(m, "opus") || strings.Contains(m, "sonnet-4-7") || strings.Contains(m, "sonnet-4-6"):
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

// containsToken reports whether s contains any of the needles bounded by
// start/end of string or one of `-_.:/`. Prevents `phi` from matching
// `alphi-…` etc.
func containsToken(s string, needles ...string) bool {
	for _, n := range needles {
		if hasToken(s, n) {
			return true
		}
	}
	return false
}

// hasSizeToken is containsToken specialised for parameter-size tokens. The
// char before the size must be a separator AND not a digit; the char after
// must also be a separator. Rejects `27b`, `mistral-87b`. Accepts `-7b-`,
// `_7b`, `7b` at end of string.
func hasSizeToken(s string, sizes ...string) bool {
	for _, sz := range sizes {
		i := 0
		for i < len(s) {
			j := strings.Index(s[i:], sz)
			if j < 0 {
				break
			}
			pos := i + j
			var before, after byte
			if pos > 0 {
				before = s[pos-1]
			}
			if pos+len(sz) < len(s) {
				after = s[pos+len(sz)]
			}
			if !isDigit(before) && isBoundary(before) && isBoundary(after) {
				return true
			}
			i = pos + 1
		}
	}
	return false
}

func hasToken(s, n string) bool {
	i := 0
	for i < len(s) {
		j := strings.Index(s[i:], n)
		if j < 0 {
			return false
		}
		pos := i + j
		var before, after byte
		if pos > 0 {
			before = s[pos-1]
		}
		if pos+len(n) < len(s) {
			after = s[pos+len(n)]
		}
		if isBoundary(before) && isBoundary(after) {
			return true
		}
		i = pos + 1
	}
	return false
}

func isBoundary(b byte) bool {
	if b == 0 {
		return true
	}
	switch b {
	case '-', '_', '.', ':', '/', ' ':
		return true
	}
	return false
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

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
	// profile-level override for recap (tiny forces off to avoid extra round-trip).
	if p.ShowRecap != nil {
		c.ShowRecap = *p.ShowRecap
	}
	return c
}
