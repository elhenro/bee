// Package loop also provides the token-estimation seam used by compaction,
// budget checks, and any caller that needs a conversation size in tokens.
package loop

import (
	"encoding/json"

	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// TokenEstimator turns a message slice into an approximate token count. It is
// the single seam for "how big is this context?" — provider-agnostic by
// design, so a provider-specific counter (or a real tokenizer) can be dropped
// in later without touching call sites.
//
// EstimateMessages reports the cost of the message content alone.
// EstimateContext reports the cost of the same messages as an assembled
// context; for the default heuristic the two are equal, while a provider-aware
// estimator may fold in framing/overhead that only exists in the wire request.
type TokenEstimator interface {
	EstimateMessages(msgs []types.Message) int
	EstimateContext(msgs []types.Message) int
}

// heuristicEstimator is the built-in char/4 estimator. It is stateless, so the
// zero value is ready to use and safe to share across goroutines.
type heuristicEstimator struct{}

// EstimateMessages sums the per-message heuristic across msgs.
func (heuristicEstimator) EstimateMessages(msgs []types.Message) int {
	total := 0
	for _, m := range msgs {
		total += estimateMessageTokens(m)
	}
	return total
}

// EstimateContext estimates the assembled context. The char/4 heuristic has no
// separate framing cost to add on top of message content, so this matches
// EstimateMessages; a provider-aware implementation may differ.
func (heuristicEstimator) EstimateContext(msgs []types.Message) int {
	return heuristicEstimator{}.EstimateMessages(msgs)
}

// DefaultTokenEstimator returns the built-in heuristic estimator.
func DefaultTokenEstimator() TokenEstimator { return heuristicEstimator{} }

// defaultTokenEstimator is the process-wide seam the loop's compaction and
// budget checks read. It defaults to the heuristic; swap it at startup or in
// tests to inject a different accounting scheme.
var defaultTokenEstimator TokenEstimator = heuristicEstimator{}

// estimateMessageTokens approximates the token cost of one message by
// summing every content block — not just text. Tool output dominates real
// conversations, so ignoring BlockToolResult under-estimates by orders of
// magnitude on tool-heavy turns.
func estimateMessageTokens(m types.Message) int {
	total := 0
	for _, c := range m.Content {
		switch c.Type {
		case types.BlockText, types.BlockThinking:
			total += prompt.EstimateTokens(c.Text)
		case types.BlockToolUse:
			if c.Use != nil {
				if b, err := json.Marshal(c.Use.Input); err == nil {
					total += prompt.EstimateTokens(string(b))
				}
				total += prompt.EstimateTokens(c.Use.Name)
			}
		case types.BlockToolResult:
			if c.Result != nil {
				total += prompt.EstimateTokens(c.Result.Content)
			}
		}
	}
	return total
}