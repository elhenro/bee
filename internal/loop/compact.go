// Package loop also provides conversation compaction helpers.
package loop

import (
	"context"
	"encoding/json"
	"errors"
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
// derived from estimateMessagesTokens so they line up with the
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
		BeforeTokens: estimateMessagesTokens(msgs),
	}
	// keep hands msgs back untouched
	keep := func(err error) ([]types.Message, CompactStats, error) {
		stats.Duration = time.Since(start)
		return msgs, stats, err
	}
	unchanged := func() ([]types.Message, CompactStats, error) {
		stats.AfterMsgs, stats.AfterTokens = stats.BeforeMsgs, stats.BeforeTokens
		return keep(nil)
	}
	if len(msgs) <= PreserveTail {
		return unchanged()
	}
	cut := len(msgs) - PreserveTail
	// never start the preserved tail on an orphaned tool result: a message
	// carrying tool_result blocks wire-translates to a role:"tool" message,
	// which the provider rejects unless the assistant tool_use that issued it
	// precedes it. walk the boundary back so the pair stays together.
	for cut > 0 && hasBlock(msgs[cut], types.BlockToolResult) {
		cut--
	}
	if cut <= 0 {
		// whole older slice is one tool exchange; nothing safe to summarize.
		return unchanged()
	}
	older := msgs[:cut]
	preserved := msgs[cut:]

	var b strings.Builder
	b.WriteString("Summarize this coding-agent conversation tersely. Keep file paths, key decisions, errors, and TODOs. Drop chatter. Caveman compress.\n\n")
	b.WriteString(renderForSummary(older))
	sum, err := streamText(ctx, p, llm.Request{
		Model:    model,
		System:   "You compress conversation history losslessly for the parts that matter.",
		Messages: []types.Message{plainMessage(types.RoleUser, b.String())},
	})
	if err != nil {
		return keep(err)
	}
	if err := ctx.Err(); err != nil {
		return keep(err)
	}
	// a reasoning-only or empty stream would replace the whole history with
	// a bare marker; fail so the caller keeps the original messages.
	if strings.TrimSpace(sum) == "" {
		return keep(errors.New("compact: empty summary"))
	}
	out := append([]types.Message{plainMessage(types.RoleUser, "[compacted history]\n"+sum)}, preserved...)
	stats.AfterMsgs = len(out)
	stats.AfterTokens = estimateMessagesTokens(out)
	stats.Duration = time.Since(start)
	return out, stats, nil
}

// streamText runs req on p and returns the concatenated text deltas. A stream
// error ends the pass with the text so far; the channel is drained either way
// so the provider goroutine exits cleanly.
func streamText(ctx context.Context, p llm.Provider, req llm.Request) (string, error) {
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var (
		sum       strings.Builder
		streamErr error
	)
	for ev := range ch {
		switch {
		case ev.Type == llm.EventTextDelta:
			sum.WriteString(ev.Delta)
		case ev.Type == llm.EventError && ev.Err != nil && streamErr == nil:
			streamErr = ev.Err
		}
	}
	return sum.String(), streamErr
}

// ShouldAutoCompact returns true if assembled prompt + history exceeds
// budget * threshold. budget<=0 disables. Estimate-only variant — kept for
// callers that don't have a recent provider usage report. Prefer
// ShouldAutoCompactWithUsage when an EventDone usage is available.
func ShouldAutoCompact(sys string, msgs []types.Message, budget int, threshold float64) bool {
	return ShouldAutoCompactWithUsage(sys, msgs, 0, budget, threshold)
}

// ShouldAutoCompactWithUsage trips when input-token usage crosses
// budget*threshold. The larger of the provider's last reported input count
// (accurate for the request it measured, works for any provider that reports
// usage) and a heuristic estimate over sys + every content block (text,
// thinking, tool_use input, tool_result content) wins, so content appended
// after the measured request still counts.
//
// budget<=0 or threshold<=0 disables.
func ShouldAutoCompactWithUsage(sys string, msgs []types.Message, actualInputTokens, budget int, threshold float64) bool {
	return compactPressure(sys, msgs, actualInputTokens, budget, threshold) > 1
}

// compactPressure is ShouldAutoCompactWithUsage's measure before the cut: the
// token count as a multiple of the compaction trigger (budget*threshold), so 1 is
// the trigger itself. 0 means disabled (budget<=0 or threshold<=0).
func compactPressure(sys string, msgs []types.Message, actualInputTokens, budget int, threshold float64) float64 {
	if budget <= 0 || threshold <= 0 {
		return 0
	}
	// the reported count predates messages appended since that request (the
	// assistant reply and fresh tool results), so never trust it below the
	// estimate: one huge tool output would otherwise overflow the window.
	total := prompt.EstimateTokens(sys) + estimateMessagesTokens(msgs)
	if actualInputTokens > total {
		total = actualInputTokens
	}
	return float64(total) / (float64(budget) * threshold)
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

// renderForSummary lists msgs as "[role] text" lines for the summarizer.
func renderForSummary(msgs []types.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if txt := flattenForSummary(m); txt != "" {
			fmt.Fprintf(&b, "[%s] %s\n", m.Role, txt)
		}
	}
	return b.String()
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
// was elided rather than empty.
func truncate(s string, n int) string { return clipRunes(s, n, "…[truncated]") }

// clipRunes cuts s to n runes and appends mark when it was longer. Ranges by
// rune so the cut lands on a rune boundary — slicing by byte could split a
// multi-byte rune into invalid UTF-8.
func clipRunes(s string, n int, mark string) string {
	count := 0
	for i := range s {
		if count == n {
			return s[:i] + mark
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
	return estimateMessagesTokens(older) >= max(budget/10, 2000)
}
