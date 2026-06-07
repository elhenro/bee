// Package loop also provides conversation compaction helpers.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/prompt"
	"github.com/elhenro/bee/internal/types"
)

// PreserveTail is the number of trailing messages kept verbatim during compaction.
const PreserveTail = 4

// CompactStats reports what a Compact call achieved. Duration spans the
// LLM summarization plus token accounting. Token figures are estimates
// derived from estimateMessageTokens so they line up with the
// auto-compact trigger heuristic.
type CompactStats struct {
	BeforeMsgs   int
	AfterMsgs    int
	BeforeTokens int
	AfterTokens  int
	Duration     time.Duration
}

// Compact summarizes msgs[:-PreserveTail] using provider into a single user
// message containing "[compacted history]\n<summary>" and returns the new slice
// plus stats describing the size delta and elapsed time.
// Returns the input unchanged if it has PreserveTail or fewer messages.
func Compact(ctx context.Context, p llm.Provider, model string, msgs []types.Message) ([]types.Message, CompactStats, error) {
	start := time.Now()
	stats := CompactStats{
		BeforeMsgs:   len(msgs),
		BeforeTokens: totalTokens(msgs),
	}
	if len(msgs) <= PreserveTail {
		stats.AfterMsgs = stats.BeforeMsgs
		stats.AfterTokens = stats.BeforeTokens
		stats.Duration = time.Since(start)
		return msgs, stats, nil
	}
	cut := len(msgs) - PreserveTail
	// never start the preserved tail on an orphaned tool result: a message
	// carrying tool_result blocks wire-translates to a role:"tool" message,
	// which the provider rejects unless the assistant tool_use that issued it
	// precedes it. walk the boundary back so the pair stays together.
	for cut > 0 && hasToolResult(msgs[cut]) {
		cut--
	}
	if cut <= 0 {
		// whole older slice is one tool exchange; nothing safe to summarize.
		stats.AfterMsgs = stats.BeforeMsgs
		stats.AfterTokens = stats.BeforeTokens
		stats.Duration = time.Since(start)
		return msgs, stats, nil
	}
	older := msgs[:cut]
	preserved := msgs[cut:]

	var b strings.Builder
	b.WriteString("Summarize this coding-agent conversation tersely. Keep file paths, key decisions, errors, and TODOs. Drop chatter. Caveman compress.\n\n")
	for _, m := range older {
		txt := flattenForSummary(m)
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
		stats.Duration = time.Since(start)
		return msgs, stats, err
	}
	var sum strings.Builder
	for ev := range ch {
		if ev.Type == llm.EventTextDelta {
			sum.WriteString(ev.Delta)
		}
		if ev.Type == llm.EventError && ev.Err != nil {
			stats.Duration = time.Since(start)
			return msgs, stats, ev.Err
		}
	}
	summaryMsg := types.Message{
		Role:    types.RoleUser,
		Content: []types.ContentBlock{{Type: types.BlockText, Text: "[compacted history]\n" + sum.String()}},
	}
	out := append([]types.Message{summaryMsg}, preserved...)
	stats.AfterMsgs = len(out)
	stats.AfterTokens = totalTokens(out)
	stats.Duration = time.Since(start)
	return out, stats, nil
}

// hasToolResult reports whether m carries any tool_result block. Such a message
// becomes a standalone role:"tool" wire message that is only valid when the
// assistant tool_use it answers immediately precedes it.
func hasToolResult(m types.Message) bool {
	for _, c := range m.Content {
		if c.Type == types.BlockToolResult {
			return true
		}
	}
	return false
}

// totalTokens sums estimateMessageTokens across a slice. Same heuristic the
// auto-compact trigger uses, so reported "size now" matches what the trigger sees.
func totalTokens(msgs []types.Message) int {
	t := 0
	for _, m := range msgs {
		t += estimateMessageTokens(m)
	}
	return t
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

// summaryToolResultCap bounds how much of a single tool result feeds the
// summarizer. Tool output (file reads, command dumps) dominates a coding
// session and can be huge; the head carries the signal (what was found),
// so cap per-result to keep the summarization prompt — and the FastModel
// that runs it — from drowning in raw dumps.
const summaryToolResultCap = 1500

// flattenForSummary renders one message for the summarizer. Unlike a text-only
// flatten it includes tool calls and tool results — the bulk of what a coding
// agent actually did and learned — so the summary isn't built from chatter
// alone. Thinking blocks are skipped: they're scratch reasoning (and the source
// of context bloat), not facts worth carrying forward.
func flattenForSummary(m types.Message) string {
	var b strings.Builder
	for _, c := range m.Content {
		switch c.Type {
		case types.BlockText:
			b.WriteString(c.Text)
			b.WriteString(" ")
		case types.BlockToolUse:
			if c.Use != nil {
				fmt.Fprintf(&b, "→%s(%s) ", c.Use.Name, truncate(toolInputBrief(c.Use.Input), summaryToolResultCap))
			}
		case types.BlockToolResult:
			if c.Result != nil {
				fmt.Fprintf(&b, "←%s ", truncate(c.Result.Content, summaryToolResultCap))
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// toolInputBrief renders a tool-call input map as compact JSON, empty on error.
func toolInputBrief(in any) string {
	b, err := json.Marshal(in)
	if err != nil {
		return ""
	}
	return string(b)
}

// truncate clips s to n runes, marking the cut so the summarizer knows output
// was elided rather than empty. Ranges by rune so the cut lands on a rune
// boundary — slicing by byte could split a multi-byte rune into invalid UTF-8.
func truncate(s string, n int) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + "…[truncated]"
		}
		count++
	}
	return s
}

// compactWorthwhile reports whether auto-compaction can actually reclaim
// meaningful budget. Compaction only rewrites the older slice (everything
// before the verbatim PreserveTail); it never touches the fixed overhead —
// system prompt + tool schemas — or the preserved tail. When the older slice
// is already small, re-compacting burns a summarization LLM call every turn
// for no gain (the over-budget signal is coming from overhead bee can't
// shrink). Suppress the trigger in that state and let the token-budget cap
// handle a genuinely wedged run. budget<=0 disables the floor (unknown window).
func compactWorthwhile(msgs []types.Message, budget int) bool {
	if len(msgs) <= PreserveTail {
		return false
	}
	older := msgs[:len(msgs)-PreserveTail]
	floor := budget / 10
	if floor < 2000 {
		floor = 2000
	}
	return totalTokens(older) >= floor
}
