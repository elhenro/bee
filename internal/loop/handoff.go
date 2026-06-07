package loop

import (
	"context"
	"fmt"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// handoffSystem is the summarizer's stance while building a rescue brief.
const handoffSystem = "You write tight handoff briefs so a stronger model can take over a stuck coding task."

// handoffPreamble frames the brief for the incoming bigger model.
const handoffPreamble = "you're a stronger model taking over a coding task a smaller model got stuck on. finish the original goal below. the smaller model's attempt is summarized for context — don't assume its approach was right; re-evaluate.\n"

// Handoff builds a rescue brief from the supplied in-memory transcript using
// the fast/compaction model and the CURRENT provider. Call this BEFORE
// switching to the big model so the cheap, already-warm provider does the
// distillation — never the slow big model on the full confused history.
func (e *Engine) Handoff(ctx context.Context, msgs []types.Message, partial, stall string) (string, error) {
	return BuildHandoff(ctx, e.Provider, compactionModel(e.Cfg), msgs, partial, stall)
}

// BuildHandoff renders the prompt a bigger model receives when taking over a
// stuck small-model run: framing + the original goal (verbatim) + a terse
// summary of the confused middle (summarized with p/model) + the stall signal
// + the last PreserveTail turns + any interrupted partial output. stall and
// partial may be empty. The middle summary uses the same streamed-summary path
// as Compact, so it should run on the small/fast model BEFORE switching.
func BuildHandoff(ctx context.Context, p llm.Provider, model string, msgs []types.Message, partial, stall string) (string, error) {
	original := firstUserText(msgs)

	// middle = everything before the verbatim tail; tail stays raw so the big
	// model sees the exact state it must continue from, not a lossy gloss.
	var middle, tail []types.Message
	if len(msgs) > PreserveTail {
		tail = msgs[len(msgs)-PreserveTail:]
		middle = msgs[:len(msgs)-PreserveTail]
	} else {
		tail = msgs
	}

	summary, err := summarizeMiddle(ctx, p, model, middle)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(handoffPreamble)
	b.WriteString("\n## original task\n")
	if original != "" {
		b.WriteString(original)
	} else {
		b.WriteString("(none recorded)")
	}
	b.WriteString("\n")
	if summary != "" {
		b.WriteString("\n## what was tried\n")
		b.WriteString(summary)
		b.WriteString("\n")
	}
	if stall != "" {
		b.WriteString("\n## where it got stuck\n")
		b.WriteString(stall)
		b.WriteString("\n")
	}
	if tailTxt := flattenTail(tail, partial); tailTxt != "" {
		b.WriteString("\n## last steps\n")
		b.WriteString(tailTxt)
		b.WriteString("\n")
	}
	b.WriteString("\ntake over and finish the original task.")
	return strings.TrimSpace(b.String()), nil
}

// summarizeMiddle streams a terse summary of the confused middle. Empty middle
// (short session) yields an empty summary — the verbatim tail carries it all.
func summarizeMiddle(ctx context.Context, p llm.Provider, model string, middle []types.Message) (string, error) {
	if len(middle) == 0 {
		return "", nil
	}
	var b strings.Builder
	b.WriteString("Summarize what this coding agent attempted and learned, tersely. Keep file paths, key decisions, errors, and TODOs. Drop chatter.\n\n")
	for _, m := range middle {
		txt := flattenForSummary(m)
		if txt == "" {
			continue
		}
		fmt.Fprintf(&b, "[%s] %s\n", m.Role, txt)
	}
	req := llm.Request{
		Model:  model,
		System: handoffSystem,
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: b.String()}},
		}},
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return "", err
	}
	var sum strings.Builder
	for ev := range ch {
		if ev.Type == llm.EventTextDelta {
			sum.WriteString(ev.Delta)
		}
		if ev.Type == llm.EventError && ev.Err != nil {
			return "", ev.Err
		}
	}
	return strings.TrimSpace(sum.String()), nil
}

// firstUserText returns the first non-empty user text block — the original task
// anchor that must survive the handoff losslessly.
func firstUserText(msgs []types.Message) string {
	for _, m := range msgs {
		if m.Role != types.RoleUser {
			continue
		}
		for _, c := range m.Content {
			if c.Type == types.BlockText && strings.TrimSpace(c.Text) != "" {
				return strings.TrimSpace(c.Text)
			}
		}
	}
	return ""
}

// flattenTail renders the preserved tail plus any interrupted partial output,
// capping each entry so a huge tool dump can't drown the brief.
func flattenTail(tail []types.Message, partial string) string {
	var b strings.Builder
	for _, m := range tail {
		if txt := flattenForSummary(m); txt != "" {
			fmt.Fprintf(&b, "[%s] %s\n", m.Role, truncate(txt, summaryToolResultCap))
		}
	}
	if p := strings.TrimSpace(partial); p != "" {
		fmt.Fprintf(&b, "[assistant — interrupted mid-output] %s\n", truncate(p, summaryToolResultCap))
	}
	return strings.TrimSpace(b.String())
}
