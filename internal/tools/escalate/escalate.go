// Package escalate implements the escalate tool: the model's explicit exit
// door when it's stuck and a human should take over. Calling it returns
// loop.ErrEscalate via a typed error so the loop bails cleanly instead of
// silently looping until iter-cap.
package escalate

import (
	"context"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "escalate"

// Error is the typed sentinel returned by Run. callers match it via
// errors.As; loop.ErrEscalate provides a string-comparable shim.
type Error struct {
	Reason     string
	NextAction string
	// Options are discrete choices for the user to pick from. When set, the
	// TUI renders an interactive picker (arrow/number select) instead of
	// free-text next-action prose.
	Options []string
}

func (e *Error) Error() string {
	if e.NextAction == "" {
		return "escalate: " + e.Reason
	}
	return "escalate: " + e.Reason + " — next: " + e.NextAction
}

// Tool is the escalate tool.
type Tool struct{}

// New returns an escalate tool.
func New() *Tool { return &Tool{} }

// Spec advertises the tool to the model. Small models do better when the
// description is concrete and gives one clear use case.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          toolName,
		Description:   "Last resort. Call ONLY when no tool action remains: the same approach failed several times in a row, or you need a decision only the user can make (credentials, ambiguous intent, irreversible choice). Do NOT escalate just because a task is large, multi-step, or unverified — if you can still read files, edit code, run commands, or search, keep working and finish what you can first. Args: reason (required, why you're stuck), options (preferred — a short list of distinct choices the user can pick from; the UI shows them as a selectable menu), suggested_next_action (optional free text, use only when there are no discrete options). When you have options, put them in the options array — do NOT also restate them in suggested_next_action. Calling this stops the loop.",
		PromptSnippet: "Stop and ask the user — only when no tool action remains",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason":                map[string]any{"type": "string", "minLength": 1},
				"suggested_next_action": map[string]any{"type": "string"},
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "discrete choices for the user to pick from",
				},
			},
			"required": []any{"reason"},
		},
	}
}

// Run returns a typed *Error so the loop's tool-dispatch can recognize it via
// errors.As and propagate without wrapping the value in a ToolResult.
func (t *Tool) Run(_ context.Context, in map[string]any) (tools.Result, error) {
	reason, _ := in["reason"].(string)
	if reason == "" {
		reason = "(no reason provided)"
	}
	next, _ := in["suggested_next_action"].(string)
	return tools.Result{}, &Error{Reason: reason, NextAction: next, Options: parseOptions(in["options"])}
}

// parseOptions coerces the schema's string array into []string, tolerating the
// json-decoded []any shape and dropping blank entries.
func parseOptions(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
