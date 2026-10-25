package loop

import (
	"context"
	"errors"
	"strings"

	"github.com/elhenro/bee/internal/learn"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// recapSystem asks for a short recap that also teaches: what changed and why,
// the one thing worth a reviewer's eye, and the next step. Kept to two
// sentences so it fits any provider context and bounds the side-call cost.
const recapSystem = `Write a recap of the assistant's last turn for the developer who must understand and review it.

Format: "<what changed and why>. Check: <the assumption or unverified risk to look at first>. Next: <suggested next step>."

Example:
  Snapped second-story midFloor to the wall base because the old offset left a gap. Check: only the square-room case was tried. Next: test an L-shaped floor.

Rules:
- Max 300 characters, plain sentences, no markdown, no quotes, no prefix labels.
- Past tense, concrete verbs. Name the reason, not just the action.
- "Check:" names one specific thing, omit it if nothing is uncertain.
- "Next:" suggests one specific follow-up.
- No meta-commentary. No "I need to", "The user wants me", etc.
- Reasons for edits may be listed under "Reasons:"; use them, do not invent others.
- If the turn was a greeting, question, or no work, reply "skip".`

// recapMaxInput caps how much of the assistant's text we feed back into
// the side call. Long turns get truncated; the tail usually has the
// summary the model just wrote anyway.
const recapMaxInput = 4000

// RecapResult carries the parsed recap line plus any error/skip cause so
// callers can render a visible diagnostic when generation fails or the
// model skipped. Text is empty when Err is set or Skipped is true.
type RecapResult struct {
	Text    string
	Skipped bool
	Err     error
}

// GenerateRecap calls the same provider/model with a short summarising
// prompt and returns the assistant's recap line. Empty Text + nil Err +
// Skipped=true means the model emitted the "skip" sentinel or the turn
// had no assistant text. Stream enabled for parity with classifier —
// adapters without streaming still aggregate to a single text response.
func GenerateRecap(ctx context.Context, p llm.Provider, model string, msgs []types.Message) RecapResult {
	if p == nil {
		return RecapResult{Err: errors.New("no provider")}
	}
	text := extractRecapInput(msgs)
	if text == "" {
		return RecapResult{Skipped: true}
	}
	if len(text) > recapMaxInput {
		// keep the tail — final paragraph usually carries the conclusion.
		text = text[len(text)-recapMaxInput:]
	}
	req := llm.Request{
		Model:       model,
		System:      recapSystem,
		Messages:    []types.Message{plainMessage(types.RoleUser, text)},
		MaxTokens:   120,
		Temperature: 0,
		Stream:      true,
	}
	out, streamErr := streamText(ctx, p, req)
	raw := strings.TrimSpace(out)
	if raw == "" {
		if streamErr != nil {
			return RecapResult{Err: streamErr}
		}
		return RecapResult{Skipped: true}
	}
	parsed := parseRecapOutput(raw)
	if parsed == "" {
		return RecapResult{Skipped: true}
	}
	return RecapResult{Text: parsed}
}

// extractRecapInput concatenates plain text from the trailing assistant
// message(s). Tool uses, tool results, and thinking blocks are skipped:
// the recap is about what the agent told the user, not internal reasoning.
// Returns "" when no usable text was found.
//
// Walks backwards to find the cutoff (the most recent user turn), then
// emits text forward from there so chronological order is preserved. A
// previous implementation walked backwards while appending, which reversed
// the order and produced newest-first recap input.
func extractRecapInput(msgs []types.Message) string {
	start := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == types.RoleUser {
			start = i + 1
			break
		}
	}
	var b strings.Builder
	for i := start; i < len(msgs); i++ {
		m := msgs[i]
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, c := range m.Content {
			if c.Type == types.BlockText && c.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(c.Text)
			}
		}
	}
	if reasons := editReasons(msgs[start:]); reasons != "" {
		b.WriteString("\n\nReasons:\n" + reasons)
	}
	return strings.TrimSpace(b.String())
}

// editReasons lists "path: why" for each mutation in the turn that carried a
// why, so the recap can state the real rationale instead of guessing it.
func editReasons(msgs []types.Message) string {
	var b strings.Builder
	for _, c := range learn.Touched(msgs, "") {
		for _, w := range c.Whys {
			b.WriteString("- " + c.Path + ": " + w + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// parseRecapOutput sanitises the model's raw response: strips quoting
// and prefix labels, collapses to one line, drops the skip sentinel.
func parseRecapOutput(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// drop common prefixes the model occasionally adds despite the rules.
	lower := strings.ToLower(s)
	for _, prefix := range []string{"recap:", "summary:"} {
		if strings.HasPrefix(lower, prefix) {
			s = strings.TrimSpace(s[len(prefix):])
			lower = strings.ToLower(s)
		}
	}
	s = strings.Trim(s, "\"'`")
	// skip sentinel — model sometimes writes "skip", "skipped", "(skip)",
	// or "(skipped)" despite the rules; treat any of these as no recap.
	core := strings.ToLower(strings.TrimSpace(s))
	core = strings.Trim(core, "().[]")
	if core == "skip" || core == "skipped" {
		return ""
	}
	// collapse newlines to spaces so renderers can show as a single dim line.
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}
