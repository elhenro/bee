package config

import (
	"slices"
	"strings"
)

// Profile carries the small-model-friendly context budgets defined in
// PLAN §6b. Profile values are applied onto Config top-level fields by
// ApplyProfile.
type Profile struct {
	SystemPromptBudget       int    `toml:"system_prompt_budget"`
	MemoryTopK               int    `toml:"memory_top_k"`
	MemoryBodyChars          int    `toml:"memory_body_chars"` // per-memory body cap; 0 → unbounded
	ToolDescChars            int    `toml:"tool_desc_chars"`
	SkillManifestChars       int    `toml:"skill_manifest_chars"`
	Caveman                  string `toml:"caveman"`
	MaxIterations            int    `toml:"max_iterations"`              // iter cap override; 0 → inherit cfg.MaxIterations; negative → unlimited
	NoMutationStallThreshold int    `toml:"no_mutation_stall_threshold"` // streak threshold for stall warning; 0 → off (opt-in)
	// ToolFormat selects how tools are advertised. "" = auto: local providers
	// resolve to "json", hosted providers to native tool_calls. "json" = wrap
	// with JSONModeProvider: tool calls go through grammar-constrained JSON
	// (response_format json_schema), so on grammar-capable servers (ollama
	// ≥0.5, llama.cpp, MLX/xgrammar) a malformed call is impossible; servers
	// that reject the field degrade to prompt-instructed JSON. "xml" = wrap
	// with TextModeProvider (text advert + `<name>{...}</name>` parsing).
	// "native" forces the native tool_calls channel everywhere.
	ToolFormat string `toml:"tool_format"`
	// ToolOutputTokens caps a single tool-result payload in token estimates
	// (chars/4). 0 → fall back to per-tool default in internal/tools. Tiny
	// profile uses a much smaller cap so one fat `read` doesn't blow a 4-8k
	// local-model context in a single turn.
	ToolOutputTokens int `toml:"tool_output_tokens"`
	// SkipApplyPatch removes the apply_patch tool from the manifest. Tiny
	// models mis-emit unified diffs (wrong context lines, off-by-one hunks);
	// edit_diff + hashline_edit + write cover the same mutation surface
	// with shapes small models hit reliably.
	SkipApplyPatch bool `toml:"skip_apply_patch"`
	// ReadDefaultLines is the default line slice when the model omits `lines`.
	// 0 → tool's own default. Tiny uses 100 to stop one read torching the window.
	ReadDefaultLines int `toml:"read_default_lines"`
	// ReadMaxLines caps the largest slice a single read can return. 0 → tool default.
	ReadMaxLines int `toml:"read_max_lines"`
	// GrepMaxMatches caps grep result count per call. 0 → tool default.
	GrepMaxMatches int `toml:"grep_max_matches"`
	// Temperature / TopP pin sampling for this profile. Zero values mean
	// "use provider default". Tiny pins temperature=0 (deterministic tool
	// turns) with top_p=0.8 to keep 4-bit MoE outputs anchored.
	Temperature float64 `toml:"temperature"`
	TopP        float64 `toml:"top_p"`
	// ShowRecap, when non-nil, overrides Config.ShowRecap on on-host
	// providers. Tiny sets false so the side-LLM recap round-trip doesn't
	// double turn latency on slow local runs.
	ShowRecap *bool `toml:"show_recap"`
	// StateCard replaces the rolling transcript in each LLM request with a
	// harness-maintained state card (goal, files touched, recent actions,
	// last error) plus the last few raw messages. Request size stays near
	// constant regardless of run length — no compaction cliff on 4-8k local
	// windows, and auto-compact is skipped while active. The on-disk session
	// keeps the full history. Opt-in.
	StateCard bool `toml:"state_card"`

	// Safety carries per-profile calibration applied on top of the global
	// sandbox + approval config. Tiny profile defaults to requiring the user
	// to re-confirm destructive ops even after AllowSession was granted, so
	// a hallucinating small model can't piggyback on one "yes" to run more
	// destructive commands later.
	Safety ProfileSafety `toml:"safety"`
}

// ProfileSafety mirrors safety.ProfileSafety in the config schema. Kept here
// (rather than importing the safety package) so config has no upward dep on
// the safety package; the tiny defaults live inline in defaults.go.
type ProfileSafety struct {
	ExtraSafeCommands     []string `toml:"extra_safe_commands"`
	RequireApprovalKeys   []string `toml:"require_approval_keys"`
	WarnOnDuplicateWrites bool     `toml:"warn_on_duplicate_writes"`
}

// boolPtr is the canonical helper for the optional bool overrides in Profile.
func boolPtr(b bool) *bool { return &b }

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
	case containsAny(m, "a3b", "a7b", "-moe", "moe-", "coder-30b"),
		containsToken(m, "flash", "mini", "nano", "haiku", "phi", "1b", "3b", "7b", "8b"):
		return "tiny"
	case containsToken(m, "opus") || strings.Contains(m, "sonnet-4-7") || strings.Contains(m, "sonnet-4-6"):
		return "large"
	default:
		return "normal"
	}
}

func containsAny(s string, needles ...string) bool {
	return slices.ContainsFunc(needles, func(n string) bool { return strings.Contains(s, n) })
}

// containsToken reports whether s contains any of the needles bounded by
// start/end of string or one of `-_.:/ `. Prevents `phi` from matching
// `alphi-…` and `8b` from matching `mistral-87b` or `27b`.
func containsToken(s string, needles ...string) bool {
	return slices.ContainsFunc(needles, func(n string) bool { return hasToken(s, n) })
}

func hasToken(s, n string) bool {
	for i := 0; i < len(s); {
		j := strings.Index(s[i:], n)
		if j < 0 {
			return false
		}
		pos := i + j
		if isBoundary(s, pos-1) && isBoundary(s, pos+len(n)) {
			return true
		}
		i = pos + 1
	}
	return false
}

// isBoundary reports whether s[i] is a token edge: out of range or a separator.
func isBoundary(s string, i int) bool {
	return i < 0 || i >= len(s) || strings.IndexByte("-_.:/ ", s[i]) >= 0
}

// ApplyProfile overlays the active profile onto top-level Config fields
// that are still zero. Explicit user choices win over profile defaults;
// the profile only fills gaps. Also resolves c.Profile == "auto" in place
// so downstream code sees the concrete name.
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
	// profile-level recap override applies to on-host runs only, where the extra
	// round-trip is slow. hosted tiny models are fast, and the recap teaches.
	if p.ShowRecap != nil && IsLocalProvider(c.DefaultProvider) {
		c.ShowRecap = *p.ShowRecap
	}
	return c
}
