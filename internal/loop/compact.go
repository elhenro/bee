// Package loop also provides conversation compaction helpers.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// PreserveTail is the number of trailing messages kept verbatim during compaction.
const PreserveTail = 4

// Compact summarizes msgs[:-PreserveTail] using provider into a single user
// message containing "[compacted history]\n<summary>" and returns the new slice.
// Returns the input unchanged if it has PreserveTail or fewer messages.
func Compact(ctx context.Context, p llm.Provider, model string, msgs []types.Message) ([]types.Message, error) {
	if len(msgs) <= PreserveTail {
		return msgs, nil
	}
	cut := len(msgs) - PreserveTail
	older := msgs[:cut]
	preserved := msgs[cut:]

	var b strings.Builder
	b.WriteString("Summarize this coding-agent conversation tersely. Keep file paths, key decisions, errors, and TODOs. Drop chatter. Caveman compress.\n\n")
	for _, m := range older {
		txt := flattenText(m)
		if txt == "" {
			continue
		}
		fmt.Fprintf(&b, "[%s] %s\n", m.Role, txt)
	}
	req := llm.Request{
		Model:  model,
		System: "You compress conversation history losslessly for the parts that matter.",
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: b.String()}},
		}},
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return msgs, err
	}
	var sum strings.Builder
	for ev := range ch {
		if ev.Type == llm.EventTextDelta {
			sum.WriteString(ev.Delta)
		}
		if ev.Type == llm.EventError && ev.Err != nil {
			return msgs, ev.Err
		}
	}
	summaryMsg := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "[compacted history]\n" + sum.String()}},
	}
	out := append([]types.Message{summaryMsg}, preserved...)
	return out, nil
}

// ShouldAutoCompact returns true if assembled prompt + history exceeds
// budget * threshold. budget<=0 disables. Estimate-only variant — kept for
// callers that don't have a recent provider usage report. Prefer
// ShouldAutoCompactWithUsage when an EventDone usage is available.
func ShouldAutoCompact(sys string, msgs []types.Message, budget int, threshold float64) bool {
	return ShouldAutoCompactWithUsage(sys, msgs, 0, budget, threshold)
}

// ShouldAutoCompactWithUsage trips when input-token usage crosses
// budget*threshold. When actualInputTokens > 0 (real value from provider's
// last EventDone usage) we use it directly — most accurate signal we have
// and works for any provider that reports usage. Falls back to a heuristic
// estimate over sys + every content block (text, thinking, tool_use input,
// tool_result content) when no live count is available.
//
// budget<=0 or threshold<=0 disables.
func ShouldAutoCompactWithUsage(sys string, msgs []types.Message, actualInputTokens, budget int, threshold float64) bool {
	if budget <= 0 || threshold <= 0 {
		return false
	}
	total := actualInputTokens
	if total <= 0 {
		total = prompt.EstimateTokens(sys)
		for _, m := range msgs {
			total += estimateMessageTokens(m)
		}
	}
	return float64(total) > float64(budget)*threshold
}

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

func flattenText(m types.Message) string {
	var b strings.Builder
	for _, c := range m.Content {
		if c.Type == types.BlockText {
			b.WriteString(c.Text)
			b.WriteString(" ")
		}
	}
	return strings.TrimSpace(b.String())
}
