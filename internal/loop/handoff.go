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
	middle, tail := splitTail(msgs)
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

// BuildDeathReport renders a run that died before finishing as a `## report`
// block in the hive contract's shape (see internal/hive): status died with the
// cause, what was tried (the middle, summarized with p/model) and the last
// PreserveTail turns verbatim as what the next bee needs. Same distillation
// path as BuildHandoff with hive framing instead of rescue framing, so a dead
// worker's learning reaches the queen instead of evaporating with its
// transcript.
func BuildDeathReport(ctx context.Context, p llm.Provider, model string, msgs []types.Message, cause error) (string, error) {
	middle, tail := splitTail(msgs)
	summary, err := summarizeMiddle(ctx, p, model, middle)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("## report\nstatus: died\n")
	if cause != nil {
		fmt.Fprintf(&b, "cause: %v\n", cause)
	}
	b.WriteString("did: ")
	if summary != "" {
		b.WriteString(summary)
	} else {
		b.WriteString("(nothing recorded before it died)")
	}
	b.WriteString("\nleft: unknown, the sub-task was not finished\n")
	b.WriteString("next: last steps before death:\n")
	b.WriteString(flattenTail(tail, ""))
	return strings.TrimSpace(b.String()), nil
}

// splitTail separates the confused middle from the verbatim tail. The tail
// stays raw so the reader sees the exact state the run stopped in, not a
// lossy gloss; the middle is what gets summarized.
func splitTail(msgs []types.Message) (middle, tail []types.Message) {
	if len(msgs) > PreserveTail {
		return msgs[:len(msgs)-PreserveTail], msgs[len(msgs)-PreserveTail:]
	}
	return nil, msgs
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
	sum, err := streamText(ctx, p, llm.Request{
		Model:  model,
		System: handoffSystem,
		Messages: []types.Message{{
			Role:    types.RoleUser,
			Content: []types.ContentBlock{{Type: types.BlockText, Text: b.String()}},
		}},
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sum), nil
}

// firstUserText returns the first non-empty user text block the user wrote —
// the original task anchor that must survive the handoff losslessly.
// Compaction summaries and loop-injected turns are skipped.
func firstUserText(msgs []types.Message) string {
	for _, m := range msgs {
		if m.Role != types.RoleUser {
			continue
		}
		for _, c := range m.Content {
			if c.Type == types.BlockText && strings.TrimSpace(c.Text) != "" && !isSyntheticUserText(c.Text) {
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
