package llm

import "strings"

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

// SupportsVision reports whether modelID belongs to a family that accepts image
// content blocks. Substring match against visionModelSubstrings on both the
// full id and trailing path segment. Unknown models return false → the loop's
// vision fallback kicks in (describe-then-inject) when one is configured.
func SupportsVision(modelID string) bool {
	if modelID == "" {
		return false
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
