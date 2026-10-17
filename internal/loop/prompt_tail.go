package loop

import (
	"context"

	"github.com/elhenro/bee/internal/types"
)

// SetPromptTail installs the per-turn system-prompt tail. Method form of the
// PromptTail field so orchestrators that only hold a Runner interface (the
// hive queen handing out pheromone cards) can attach it without knowing the
// concrete engine type.
func (e *Engine) SetPromptTail(f func() string) { e.PromptTail = f }

// DeathReport distills a run that died (iteration cap, escalate, stream
// failure, …) into the exit brief the hive contract asks every worker for, so
// a dead bee's learning reaches the queen instead of evaporating with its
// transcript. Uses the fast/compaction model on the current provider.
func (e *Engine) DeathReport(ctx context.Context, msgs []types.Message, cause error) (string, error) {
	return BuildDeathReport(ctx, e.Provider, compactionModel(e.Cfg), msgs, cause)
}

// appendPromptTail appends tail to the system prompt and to its volatile
// suffix so the wire layer still finds the cache breakpoint: the static prefix
// stays byte-identical turn over turn and the tail rides after it. With no
// existing dynamic suffix the tail becomes the whole dynamic part. An empty
// tail returns the inputs unchanged.
func appendPromptTail(sys, dynamic, tail string) (string, string) {
	if tail == "" {
		return sys, dynamic
	}
	if sys == "" {
		return tail, tail
	}
	sys += "\n\n" + tail
	if dynamic == "" {
		return sys, tail
	}
	return sys, dynamic + "\n\n" + tail
}

// promptTail evaluates the hook, tolerating a nil engine hook.
func (e *Engine) promptTail() string {
	if e.PromptTail == nil {
		return ""
	}
	return e.PromptTail()
}
