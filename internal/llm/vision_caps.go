package llm

import (
	"context"
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

func hasModality(list []string, want string) bool {
	for _, m := range list {
		if strings.EqualFold(m, want) {
			return true
		}
	}
	return false
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
	tail := id
	if idx := strings.LastIndex(id, "/"); idx >= 0 {
		tail = id[idx+1:]
	}
	for _, s := range visionModelSubstrings {
		if strings.Contains(id, s) || strings.Contains(tail, s) {
			return true
		}
	}
	return false
}

// ActiveModelHasVision is SupportsVision plus one catalogue lookup when the
// name list can't vouch for the active model, so models it has never heard of
// are judged by what their provider declares. Best-effort: a failed lookup is
// retried next call, a successful one is cached by ListModels.
func ActiveModelHasVision(ctx context.Context, cfg *config.Config) bool {
	id := cfg.DefaultModel
	if SupportsVision(id) {
		return true
	}
	if _, known := liveVisionVerdict(id); known {
		return false
	}
	pc, ok := cfg.Providers[cfg.DefaultProvider]
	if !ok {
		return false
	}
	_, _ = ListModels(ctx, cfg.DefaultProvider, pc)
	return SupportsVision(id)
}
