package cost

import (
	"regexp"
	"strings"
)

// Price is per-million-token rates for one model.
type Price struct {
	InputPerM  float64
	OutputPerM float64
}

// Lookup returns pricing for (provider, model). Tries the most specific key
// first (provider+model), then bare model, then a stripped path-suffix
// (openrouter returns "vendor/model" ids), then without a snapshot date or
// "-latest" tag. Returns false when nothing matches — caller treats that as
// zero-cost.
func Lookup(provider, model string) (Price, bool) {
	if p, ok := prices[provider+"|"+model]; ok {
		return p, true
	}
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	if p, ok := prices[model]; ok {
		return p, true
	}
	// routed ids spell versions with dots (claude-sonnet-4.5), the table with dashes
	model = strings.ReplaceAll(model, ".", "-")
	if p, ok := prices[model]; ok {
		return p, true
	}
	p, ok := prices[snapshotSuffix.ReplaceAllString(model, "")]
	return p, ok
}

// snapshotSuffix matches a dated snapshot (-20250929, -2024-07-18) or a
// "-latest" alias tail, which price like the undated id.
var snapshotSuffix = regexp.MustCompile(`-(\d{8}|\d{4}-\d{2}-\d{2}|latest)$`)

// cachedFactor is the share of the input rate billed for a cache-read token.
// Only families with a documented discount get one; any other model pays the
// full rate (1), so an unknown discount never under-reports spend.
func cachedFactor(model string) float64 {
	if idx := strings.LastIndex(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}
	switch {
	case strings.HasPrefix(model, "claude-"):
		return 0.1
	case strings.HasPrefix(model, "gpt-4o"), strings.HasPrefix(model, "o1"), strings.HasPrefix(model, "o3"):
		return 0.5
	}
	return 1
}

// usd prices one request. in includes the cached tokens, which bill at the
// cached rate.
func (p Price) usd(model string, in, cached, out int) float64 {
	cached = min(max(cached, 0), max(in, 0))
	inputM := float64(in-cached) + float64(cached)*cachedFactor(model)
	return (inputM/1_000_000)*p.InputPerM + (float64(out)/1_000_000)*p.OutputPerM
}

// prices is the embedded pricing table (USD per million tokens).
// Numbers reflect public list prices as of early 2026. The TUI marks
// unpriced models with a "—" so the user knows the dollar number isn't
// gospel.
var prices = map[string]Price{
	// OpenAI
	"gpt-4o-mini": {0.15, 0.60},
	"gpt-4o":      {2.50, 10.00},
	"o1":          {15.00, 60.00},
	"o1-mini":     {3.00, 12.00},
	"o3-mini":     {1.10, 4.40},
	// Anthropic
	"claude-haiku-4-5":  {1.00, 5.00},
	"claude-sonnet-4-5": {3.00, 15.00},
	"claude-sonnet-4-6": {3.00, 15.00},
	"claude-opus-4-7":   {15.00, 75.00},
	// DeepSeek
	"deepseek-v4-flash": {0.07, 1.10},
	"deepseek-chat":     {0.27, 1.10},
	"deepseek-reasoner": {0.55, 2.19},
	// Google
	"gemini-2.0-flash": {0.10, 0.40},
	"gemini-2.5-pro":   {1.25, 10.00},
	"gemini-2.5-flash": {0.30, 2.50},
	// Groq (Llama variants are routed via groq.com)
	"llama-3.3-70b-versatile": {0.59, 0.79},
	"llama-3.1-8b-instant":    {0.05, 0.08},
	// Moonshot Kimi K2 — public list rates (USD/M). OpenRouter ids in the
	// `moonshotai/kimi-k2*` family strip to `kimi-k2*` via Lookup's path-
	// suffix fallback, so the bare slugs cover both direct and routed use.
	"kimi-k2":   {0.60, 2.50},
	"kimi-k2.5": {0.60, 2.50},
	"kimi-k2.6": {0.60, 2.50},
}
