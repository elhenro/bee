package goal

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// Verdict is the fast model's judgement on whether a goal condition is met.
type Verdict struct {
	Met    bool
	Reason string
}

// evalSystem instructs the judge. It sees only the conversation — no file or
// command access — and must reply with strict JSON.
const evalSystem = `You are a strict completion judge for an autonomous coding agent.

You can ONLY see the conversation transcript below. You have no access to files,
the filesystem, or any commands — judge solely by what the conversation surfaces.

Decide whether the CONDITION is demonstrably satisfied by the conversation. Be
conservative: if the evidence is missing, vague, or merely promised, it is NOT met.

Reply with STRICT JSON only, no prose, no markdown fences:
{"met": true|false, "reason": "<=15 words"}`

// evalMaxTranscript caps how much conversation text is fed to the judge. The
// tail is kept — recent messages carry the evidence.
const evalMaxTranscript = 6000

// evalRecentMessages bounds how many trailing messages are scanned.
const evalRecentMessages = 12

// evalMaxTokens caps the judge's reply. Reasoning-style small models narrate
// before concluding; too small a budget truncates them mid-thought so the JSON
// verdict never lands, parseVerdict fails, and the goal loop spins. Generous
// enough to let them reach the verdict — the reply is still tiny vs a work turn.
const evalMaxTokens = 512

// Evaluate asks a fast model whether condition is demonstrably met based on the
// recent conversation. Single cheap side call, no tools. On any provider/parse
// error it returns Verdict{Met:false, ...} and the error.
func Evaluate(ctx context.Context, p llm.Provider, model, condition string, recent []types.Message) (Verdict, error) {
	if p == nil || strings.TrimSpace(condition) == "" {
		return Verdict{Met: false, Reason: "no goal/provider"}, nil
	}
	transcript := buildTranscript(recent)
	user := "CONDITION:\n" + condition + "\n\nCONVERSATION (most recent last):\n" + transcript
	req := llm.Request{
		Model:  model,
		System: evalSystem,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{
				{Type: types.BlockText, Text: user},
			}},
		},
		MaxTokens:   evalMaxTokens,
		Temperature: 0,
		Stream:      true,
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return Verdict{Met: false, Reason: "eval call failed"}, err
	}
	var (
		buf       strings.Builder
		streamErr error
	)
	for ev := range ch {
		switch ev.Type {
		case llm.EventTextDelta:
			buf.WriteString(ev.Delta)
		case llm.EventError:
			if ev.Err != nil {
				streamErr = ev.Err
			}
		}
	}
	raw := strings.TrimSpace(buf.String())
	if raw == "" {
		if streamErr != nil {
			return Verdict{Met: false, Reason: "eval stream error"}, streamErr
		}
		return Verdict{Met: false, Reason: "empty eval response"}, fmt.Errorf("empty eval response")
	}
	return parseVerdict(raw)
}

// Continuation is the synthetic user message injected to keep the agent working
// when the goal is not yet met.
func Continuation(condition, reason string) string {
	return "[goal] Not satisfied yet: " + condition +
		"\nLast check: " + reason +
		"\nKeep working toward the goal. When done, state clearly what you completed."
}

// buildTranscript renders the last few messages as "role: text" lines. It
// surfaces tool activity (calls and their results) alongside prose: a goal like
// "create a file" is proven by the write tool's result, not by the agent saying
// it did so — without this the judge sees only promises and never confirms.
// Trims to the tail when over the char cap.
func buildTranscript(msgs []types.Message) string {
	start := 0
	if len(msgs) > evalRecentMessages {
		start = len(msgs) - evalRecentMessages
	}
	var b strings.Builder
	for _, m := range msgs[start:] {
		line := renderBlocks(m.Content)
		if line == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s", m.Role, line)
	}
	out := b.String()
	if len(out) > evalMaxTranscript {
		out = out[len(out)-evalMaxTranscript:]
	}
	return out
}

// blockToolInputCap bounds how much of a tool's input/result is shown to the
// judge — enough to recognize the action, not enough to swamp the transcript.
const blockToolInputCap = 200

// renderBlocks flattens one message's content into a single line: prose plus
// compact markers for tool calls and their results, so the judge sees evidence
// of actions, not just claims about them.
func renderBlocks(blocks []types.ContentBlock) string {
	var parts []string
	for _, c := range blocks {
		switch c.Type {
		case types.BlockText:
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		case types.BlockToolUse:
			if c.Use != nil {
				parts = append(parts, fmt.Sprintf("[called %s %s]",
					c.Use.Name, truncate(compactInput(c.Use.Input), blockToolInputCap)))
			}
		case types.BlockToolResult:
			if c.Result != nil {
				status := "ok"
				if c.Result.IsError {
					status = "error"
				}
				parts = append(parts, fmt.Sprintf("[%s result: %s]",
					status, truncate(strings.TrimSpace(c.Result.Content), blockToolInputCap)))
			}
		}
	}
	return strings.Join(parts, " ")
}

// compactInput renders a tool input map as "k=v" pairs, sorted for stability.
func compactInput(in map[string]any) string {
	if len(in) == 0 {
		return ""
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%v", k, in[k]))
	}
	return strings.Join(pairs, " ")
}

// parseVerdict extracts the first {...} JSON object and unmarshals it. Tolerant
// of surrounding prose or fences the model may add.
func parseVerdict(raw string) (Verdict, error) {
	obj := firstJSONObject(raw)
	if obj == "" {
		return Verdict{Met: false, Reason: "unparseable eval: " + truncate(raw, 60)}, fmt.Errorf("no json object in eval response")
	}
	var v struct {
		Met    bool   `json:"met"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(obj), &v); err != nil {
		return Verdict{Met: false, Reason: "unparseable eval: " + truncate(raw, 60)}, err
	}
	return Verdict{Met: v.Met, Reason: strings.TrimSpace(v.Reason)}, nil
}

// firstJSONObject returns the substring from the first '{' to its matching '}'.
// Returns "" when no balanced object is found.
func firstJSONObject(s string) string {
	open := strings.IndexByte(s, '{')
	if open < 0 {
		return ""
	}
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[open : i+1]
			}
		}
	}
	return ""
}
