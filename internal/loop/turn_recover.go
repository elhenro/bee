package loop

import (
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// formatNudgeMax is the budget for format-correction nudges per Run. After
// the budget is spent, looksLikeAttemptedToolCall turns still bump the slip
// streak (driving FormatStrikeError) but no further nudge fires.
const formatNudgeMax = 2

// attemptRecoveryNudge returns a synthetic user message when the assistant
// turn ended without a tool call AND without a done signal. nil = no nudge
// fits; caller should return.
//
// Two failure modes covered:
//   1. thinking-only — provider streamed reasoning then stopped without text
//      or tool_use. fires at most once per Run.
//   2. format slip — model emitted a tool-call attempt as prose (XML-ish tag
//      or JSON envelope) but the parser didn't recognize it. fires up to
//      formatNudgeMax times with escalating wording and a concrete example
//      built from the tool name the model actually tried.
func attemptRecoveryNudge(e *Engine, assistantMsg types.Message, finalText string, toolUses []types.ToolUse, specs []llm.ToolSpec) *types.Message {
	if len(toolUses) > 0 || detectDoneSignal(finalText) {
		return nil
	}
	nudgeText := ""
	switch {
	case strings.TrimSpace(finalText) == "" && hasThinkingOnly(assistantMsg) && !e.nudgedReasoningOnly:
		nudgeText = "[nudge] previous turn was reasoning-only. respond now: emit final answer or call a tool."
		e.nudgedReasoningOnly = true
	case looksLikeAttemptedToolCall(finalText, specs) && e.formatNudgeCount < formatNudgeMax:
		nudgeText = buildFormatNudge(finalText, specs, e.formatNudgeCount)
		e.formatNudgeCount++
	}
	if nudgeText == "" {
		return nil
	}
	nudge := types.Message{
		ID:       newID(),
		ParentID: assistantMsg.ID,
		Role:     types.RoleUser,
		Content:  []types.ContentBlock{{Type: types.BlockText, Text: nudgeText}},
		Time:     time.Now().UTC(),
	}
	return &nudge
}

// buildFormatNudge composes the format-correction message. Uses the actual
// tool name the model tried (extracted from finalText) plus a real arg key
// from the tool's schema so the example doesn't echo the literal `tool_name`
// placeholder that some models copy verbatim (qwen3-A3B regression).
// Iteration number drives escalation: first nudge is gentle, second is
// explicit "next slip ends the loop".
func buildFormatNudge(text string, specs []llm.ToolSpec, iter int) string {
	spec := guessAttemptedSpec(text, specs)
	example := exampleEnvelope(spec)
	if iter == 0 {
		return fmt.Sprintf("[nudge] previous turn looked like a tool call but the envelope was wrong. "+
			"tools must be invoked as `<NAME>{\"arg\":\"value\"}</NAME>` XML blocks (real tool name as the tag, "+
			"JSON args inside the body), not bare JSON or attribute-style XML. example: %s", example)
	}
	return fmt.Sprintf("[nudge] [format-slip %d/%d] envelope still wrong. one more wrong shape ends the loop. "+
		"copy this shape exactly, change only the args: %s", iter+1, formatNudgeMax, example)
}

// guessAttemptedSpec scans text for the first known tool name that appears
// as either an XML opener (`<name`) or a JSON `"name"`/`"type"` value, and
// returns the matching spec. Falls back to the first spec when nothing
// matches so the nudge always has a concrete example to show.
func guessAttemptedSpec(text string, specs []llm.ToolSpec) llm.ToolSpec {
	low := strings.ToLower(text)
	for _, s := range specs {
		name := strings.ToLower(s.Name)
		if name == "" {
			continue
		}
		if strings.Contains(low, "<"+name) || strings.Contains(low, `"`+name+`"`) {
			return s
		}
	}
	if len(specs) > 0 {
		return specs[0]
	}
	return llm.ToolSpec{Name: "tool"}
}

// exampleEnvelope renders `<NAME>{"key":"value"}</NAME>` using the spec's
// first schema property as the arg key (so the model sees a real key the
// tool actually accepts). When no schema is available, uses a stub arg.
func exampleEnvelope(spec llm.ToolSpec) string {
	key, sample := schemaSampleArg(spec)
	if key == "" {
		return fmt.Sprintf("`<%s>{\"arg\":\"value\"}</%s>`", spec.Name, spec.Name)
	}
	return fmt.Sprintf("`<%s>{\"%s\":\"%s\"}</%s>`", spec.Name, key, sample, spec.Name)
}

// schemaSampleArg pulls a representative (key, sample-value) pair from a
// tool's JSON schema. Prefers a required key; falls back to any property.
func schemaSampleArg(spec llm.ToolSpec) (string, string) {
	if spec.Schema == nil {
		return "", ""
	}
	props, _ := spec.Schema["properties"].(map[string]any)
	if len(props) == 0 {
		return "", ""
	}
	if req, ok := spec.Schema["required"].([]string); ok && len(req) > 0 {
		if _, exists := props[req[0]]; exists {
			return req[0], "value"
		}
	}
	if reqAny, ok := spec.Schema["required"].([]any); ok && len(reqAny) > 0 {
		if k, ok := reqAny[0].(string); ok {
			if _, exists := props[k]; exists {
				return k, "value"
			}
		}
	}
	for k := range props {
		return k, "value"
	}
	return "", ""
}
