package llm

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/elhenro/bee/internal/config"
)

// liveVision holds what a provider catalogue said about image input, keyed by
// lowercase full model id. It outranks the name list below: a catalogue knows
// "z-ai/glm-5.3-flash" sees images, a substring list can't.
var liveVision = struct {
	sync.Mutex
	m map[string]bool
}{m: map[string]bool{}}

// RememberVision records a catalogue's verdict on image input for modelID.
func RememberVision(modelID string, ok bool) {
	if modelID == "" {
		return
	}
	liveVision.Lock()
	defer liveVision.Unlock()
	liveVision.m[strings.ToLower(modelID)] = ok
}

// ResetLiveVision drops every learned verdict. Tests call this.
func ResetLiveVision() {
	liveVision.Lock()
	defer liveVision.Unlock()
	liveVision.m = map[string]bool{}
}

func liveVisionVerdict(modelID string) (ok, known bool) {
	liveVision.Lock()
	defer liveVision.Unlock()
	ok, known = liveVision.m[strings.ToLower(modelID)]
	return ok, known
}

// visionModelSubstrings names model families that accept image input. Matched
// case-insensitively against the full id and the trailing path segment so
// openrouter-style ids ("anthropic/claude-sonnet-4-6") resolve. Best-effort:
// unknown models return false so the loop routes images through a configured
// fallback vision model instead of sending them to a blind model.
var visionModelSubstrings = []string{
	// Anthropic 3.x/4.x are all multimodal.
	"claude-3", "claude-sonnet-4", "claude-opus-4", "claude-haiku-4",
	// OpenAI multimodal families.
	"gpt-4o", "gpt-4.1", "gpt-4-turbo", "gpt-4-vision", "gpt-5", "o3", "o4",
	// Gemini is multimodal across the board.
	"gemini",
	// Open vision models: the "-vl" tag (qwen-vl, internvl…), plus llava and
	// other common local describe models. "llama-3.2"/"llama-4" ship vision.
	"-vl", "llava", "pixtral", "moondream", "minicpm-v", "llama-3.2", "llama-4",
}

// SupportsVision reports whether modelID accepts image content blocks. A
// catalogue verdict (RememberVision) wins; otherwise substring match against
// visionModelSubstrings on both the full id and trailing path segment. Unknown
// models return false → the loop's vision fallback kicks in (describe-then-
// inject) when one is configured.
func SupportsVision(modelID string) bool {
	if modelID == "" {
		return false
	}
	if ok, known := liveVisionVerdict(modelID); known {
		return ok
	}
	id := strings.ToLower(modelID)
	return slices.ContainsFunc(visionModelSubstrings, func(s string) bool { return strings.Contains(id, s) })
}

// ModelVision reports whether the active model accepts images and whether
// that is actually known. Order: the user's own verdict ([vision.models]), a
// catalogue verdict, the name list, then one catalogue lookup. known is false
// only when none of them could say, which is when asking the user is useful.
// The lookup runs once per (provider, model); ListModels caches the result.
func ModelVision(ctx context.Context, cfg *config.Config) (has, known bool) {
	id := cfg.DefaultModel
	if v, ok := cfg.Vision.Models[id]; ok {
		return v, true
	}
	if SupportsVision(id) {
		return true, true
	}
	if v, ok := liveVisionVerdict(id); ok {
		return v, true
	}
	pc, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok {
		return false, false
	}
	// one lookup per (provider, model): this runs on every iteration of a
	// session that holds an image, so a dead catalogue must not stall each one.
	if key := "vision:" + cfg.DefaultProvider; !wasProbed(key, id) {
		markProbed(key, id)
		_, _ = ListModels(ctx, cfg.DefaultProvider, pc)
	}
	if v, ok := liveVisionVerdict(id); ok {
		return v, true
	}
	return false, false
}

// ActiveModelHasVision is ModelVision without the known flag.
func ActiveModelHasVision(ctx context.Context, cfg *config.Config) bool {
	has, _ := ModelVision(ctx, cfg)
	return has
}
