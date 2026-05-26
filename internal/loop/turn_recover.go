package loop

import (
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// attemptRecoveryNudge returns a synthetic user message when the assistant
// turn ended without a tool call AND without a done signal AND we haven't
// nudged yet this Run. nil = no nudge fits; caller should return.
//
// Two failure modes covered:
//   1. thinking-only — provider streamed reasoning then stopped without text
//      or tool_use (e.g. some hosted reasoners). nudge: ask for output.
//   2. format slip — model emitted a tool-call attempt as prose (XML-ish tag
//      or JSON envelope) but the textmode parser didn't recognize it. silent
//      termination here leaves the user staring at JSON; nudge with an
//      explicit envelope reminder instead.
func attemptRecoveryNudge(e *Engine, assistantMsg types.Message, finalText string, toolUses []types.ToolUse, specs []llm.ToolSpec) *types.Message {
	if e.nudgedReasoningOnly || len(toolUses) > 0 || detectDoneSignal(finalText) {
		return nil
	}
	nudgeText := ""
	switch {
	case strings.TrimSpace(finalText) == "" && hasThinkingOnly(assistantMsg):
		nudgeText = "[nudge] previous turn was reasoning-only. respond now: emit final answer or call a tool."
	case looksLikeAttemptedToolCall(finalText, specs):
		nudgeText = "[nudge] previous turn looked like a tool call but the envelope was wrong. tools must be invoked as `<tool_name>{\"arg\":\"value\"}</tool_name>` XML blocks, not bare JSON or function-call objects. retry with the XML envelope."
	}
	if nudgeText == "" {
		return nil
	}
	e.nudgedReasoningOnly = true
	nudge := types.Message{
		ID:       newID(),
		ParentID: assistantMsg.ID,
		Role:     types.RoleUser,
		Content:  []types.ContentBlock{{Type: types.BlockText, Text: nudgeText}},
		Time:     time.Now().UTC(),
	}
	return &nudge
}
