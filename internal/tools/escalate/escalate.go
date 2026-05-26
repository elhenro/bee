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
		Description:   "Call when you can't make progress: same approach failed multiple times, you need a decision only the user can make, or the task is outside your competence. Args: reason (required, why you're stuck), suggested_next_action (optional, what the user should try). Calling this stops the loop and surfaces the reason to the user.",
		PromptSnippet: "Stop and ask the user",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason":                map[string]any{"type": "string"},
				"suggested_next_action": map[string]any{"type": "string"},
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
	return tools.Result{}, &Error{Reason: reason, NextAction: next}
}
