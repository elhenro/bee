package config

import "strings"

// localProviderNames lists provider ids that run on-host. Used to gate
// behavior that's worth the round-trip on hosted providers but wasteful
// on slow local models (cost rendering, auto-mode classifier, etc.).
var localProviderNames = map[string]bool{
	"ollama":    true,
	"lmstudio":  true,
	"llamacpp":  true,
	"llama-cpp": true,
	"vllm":      true,
	"localai":   true,
	"omlx":      true,
}

// IsLocalProvider returns true when the named provider runs on-host. Local
// providers skip cost rendering, skip the auto-mode classifier, and use a
// 32k context floor when the model registry has no window.
func IsLocalProvider(name string) bool {
	return localProviderNames[strings.ToLower(name)]
}

// LocalMaxOutputTokens caps one response from an on-host model. Local servers
// default to the whole context window, so a rambling generation (thinking that
// never converges, not periodic enough for the repeat watchdog) runs for
// ~10-50 min at local speeds. ~6 min at 44 tok/s, room for a long file write.
const LocalMaxOutputTokens = 16384

// OutputCap returns the per-response max_tokens for the main turn. Explicit
// max_output_tokens wins (negative lifts the cap); otherwise local providers
// get LocalMaxOutputTokens and hosted ones 0 (omit, provider default).
func OutputCap(cfg Config) int {
	switch {
	case cfg.MaxOutputTokens < 0:
		return 0
	case cfg.MaxOutputTokens > 0:
		return cfg.MaxOutputTokens
	case IsLocalProvider(cfg.DefaultProvider):
		return LocalMaxOutputTokens
	}
	return 0
}
