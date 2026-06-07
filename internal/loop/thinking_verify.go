package loop

import (
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// emptyCompletionBailAt is the consecutive-empty-turn budget before the loop
// bails with EmptyCompletionError. Smaller than the repetition/truncation caps:
// an empty turn makes zero progress, so there's no point burning many iters on
// it before handing control back.
const emptyCompletionBailAt = 2

// reasoningOpeners are lowercase prefixes a model uses when it deliberates in
// the answer channel instead of inside a <think> block. Matched as a prefix of
// the trimmed output so ordinary answers that merely mention these phrases
// mid-sentence don't trip the check.
var reasoningOpeners = []string{
	"thinking process",
	"thinking:",
	"let me think",
	"let's think",
	"let me reason",
	"reasoning:",
	"first, i need to",
	"first, let me",
	"okay, let me",
	"ok, let me",
	"step 1:",
	"**step 1",
	"the user is asking",
	"the user wants",
}

// looksLikeUntaggedReasoning reports whether text is chain-of-thought emitted
// in the answer channel with no <think> tags — the failure mode of frankenmerge
// models whose reasoning structure was severed from the tag convention.
// Conservative: requires a known reasoning opener at the very start so normal
// prose isn't misclassified.
func looksLikeUntaggedReasoning(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return false
	}
	for _, p := range reasoningOpeners {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// hasThinkingBlock reports whether msg carries any reasoning block, regardless
// of whether text or tool_use blocks accompany it. Unlike hasThinkingOnly it
// doesn't require the rest of the turn to be empty.
func hasThinkingBlock(msg types.Message) bool {
	for _, b := range msg.Content {
		if b.Type == types.BlockThinking && strings.TrimSpace(b.Text) != "" {
			return true
		}
	}
	return false
}

// verifyThinkingSuppression warns once per session when effort was set to off
// (suppression requested) but the model reasoned anyway — either by streaming a
// <think> block or by deliberating in the answer channel. The signal reaches
// the server correctly; this catches models that ignore it, so the user knows
// the setting is a no-op for this model rather than silently wondering why it
// still thinks.
func (e *Engine) verifyThinkingSuppression(finalText string, msg types.Message) {
	if e == nil || e.warnedThinkingIgnored || !e.thinkingSuppressRequested {
		return
	}
	reason := ""
	switch {
	case hasThinkingBlock(msg):
		reason = "still streaming a reasoning trace"
	case looksLikeUntaggedReasoning(finalText):
		reason = "reasoning in the answer channel (untagged)"
	}
	if reason == "" {
		return
	}
	e.warnedThinkingIgnored = true
	e.warnf("effort is off but %q is %s — this model ignores thinking suppression; switch model for faster replies",
		e.Cfg.DefaultModel, reason)
}
