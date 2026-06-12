package loop

import "github.com/elhenro/bee/internal/types"

// cross-turn reasoning-loop detection. The in-stream watchdog (repeat_stream)
// cuts a single turn that runs away. But a model can also spin across turn
// boundaries: emit one near-identical block of reasoning each turn, make a
// token tool call, then repeat the same reasoning next turn — each turn staying
// just under the in-stream cut threshold. We fingerprint each turn's reasoning
// and count consecutive near-duplicates; at reasoningDupBailAt the turn loop
// injects a hard escalate nudge.
const (
	// reasoningDupSimThreshold is the Jaccard overlap (normalized substantial
	// lines) above which two turns' reasoning counts as the same.
	reasoningDupSimThreshold = 0.8
	// reasoningDupMinLines: a turn must have at least this many substantial
	// lines to be fingerprinted — short turns are noise, not loops.
	reasoningDupMinLines = 3
	// reasoningDupBailAt: consecutive duplicate-reasoning turns before the hard
	// nudge fires.
	reasoningDupBailAt = 3
)

// reasoningFingerprint collects normalized substantial lines from a turn's
// thinking + text blocks into a set. Returns nil when the turn is too thin to
// fingerprint meaningfully.
func reasoningFingerprint(content []types.ContentBlock) map[string]struct{} {
	set := map[string]struct{}{}
	for _, b := range content {
		if b.Type != types.BlockThinking && b.Type != types.BlockText {
			continue
		}
		s := b.Text
		off := 0
		for off < len(s) {
			end := len(s)
			for j := off; j < len(s); j++ {
				if s[j] == '\n' {
					end = j
					break
				}
			}
			if norm := normalizeBlockLine(s[off:end]); len(norm) >= blockMinLineLen {
				set[norm] = struct{}{}
			}
			off = end + 1
		}
	}
	if len(set) < reasoningDupMinLines {
		return nil
	}
	return set
}

// jaccard returns |a∩b| / |a∪b| for two non-empty sets.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// observeReasoningDup folds one finished turn into the cross-turn duplicate
// streak. Reports whether the streak has crossed reasoningDupBailAt (a one-shot
// signal — fires once per Run until reasoning diverges and re-crosses).
func observeReasoningDup(e *Engine, content []types.ContentBlock) bool {
	sig := reasoningFingerprint(content)
	if sig == nil {
		// thin turn: don't reset the streak (a loop interleaved with terse
		// turns is still a loop), just skip the comparison.
		return false
	}
	if e.run.lastReasoningSig != nil && jaccard(e.run.lastReasoningSig, sig) >= reasoningDupSimThreshold {
		e.run.reasoningDupStreak++
	} else {
		e.run.reasoningDupStreak = 0
		e.run.warnedReasoningDup = false
	}
	e.run.lastReasoningSig = sig
	return e.run.reasoningDupStreak >= reasoningDupBailAt
}
