// Package loop also provides the token-estimation seam used by compaction
// and budget checks.
package loop

import (
	"encoding/json"

	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// estimateMessagesTokens is the single seam for "how big is this context?":
// the char/4 heuristic summed over msgs. Provider-agnostic, so compaction and
// budget checks share one accounting.
func estimateMessagesTokens(msgs []types.Message) int {
	total := 0
	for _, m := range msgs {
		total += estimateMessageTokens(m)
	}
	return total
}

// imageTokens is a flat per-picture cost: providers bill by pixel area, not
// by the base64 length, so char/4 over the bytes would be wildly off
const imageTokens = 1500

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
		case types.BlockImage:
			total += imageTokens
		}
	}
	return total
}
