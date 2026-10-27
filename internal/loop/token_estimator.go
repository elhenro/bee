// Package loop also provides the token-estimation seam used by compaction
// and budget checks.
package loop

import (
	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// estimateMessagesTokens is the single seam for "how big is this context?":
// the char/4 heuristic summed over msgs. provider-agnostic, so compaction and
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
		case types.BlockText:
			total += prompt.EstimateTokens(c.Text)
		case types.BlockThinking:
			// only a signed block reaches the wire (the wire drops the rest),
			// so reasoning-heavy unsigned history must not trip compaction
			if c.Signature != "" {
				total += prompt.EstimateTokens(c.Text)
			}
		case types.BlockToolUse:
			if c.Use != nil {
				total += (inputChars(c.Use.Input) + len(c.Use.Name)) / 4
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

// inputChars sizes a tool-call input as its JSON would be, without encoding
// it: the estimate runs over the whole history every iteration, and
// marshaling each call there allocates for nothing.
func inputChars(v any) int {
	switch x := v.(type) {
	case string:
		return len(x) + 2
	case map[string]any:
		n := 2
		for k, e := range x {
			n += len(k) + 4 + inputChars(e)
		}
		return n
	case []any:
		n := 2
		for _, e := range x {
			n += 1 + inputChars(e)
		}
		return n
	case nil:
		return 4
	default:
		return 8 // numbers and bools
	}
}
