// Package ask_user implements the ask_user tool: the model asks the user a
// multiple-choice question and blocks on the answer. Plan-mode only — it lets
// the agent resolve ambiguity before writing a plan instead of guessing.
package ask_user

import (
	"context"
	"fmt"

	"github.com/elhenro/bee/internal/ask"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
)

const toolName = "ask_user"

// Tool wraps an ask.Asker. A nil asker falls back to Static (auto-pick), so
// the tool is always safe to register.
type Tool struct {
	asker ask.Asker
}

// New returns an ask_user tool. Pass nil for headless/autonomous runs where no
// human is present; it auto-resolves to the recommended option.
func New(a ask.Asker) *Tool {
	if a == nil {
		a = ask.Static{}
	}
	return &Tool{asker: a}
}

// Spec advertises the tool. Concrete description + one clear use case so small
// models reach for it instead of silently guessing defaults.
func (t *Tool) Spec() llm.ToolSpec {
	return llm.ToolSpec{
		Name:          toolName,
		Description:   "Ask the user to pick from concrete options when a decision is theirs to make and you can't infer it (platform, library, scope, etc.). Surfaces a clickable picker and blocks until they choose. Args: question (required), header (optional short label), options (required array of {label, description, recommended}); mark your suggested pick recommended:true. The user may also type a custom answer. Prefer this over guessing in plan mode.",
		PromptSnippet: "Ask the user to pick an option",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"question": map[string]any{"type": "string", "minLength": 1},
				"header":   map[string]any{"type": "string"},
				"options": map[string]any{
					"type":     "array",
					"minItems": 1,
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label":       map[string]any{"type": "string", "minLength": 1},
							"description": map[string]any{"type": "string"},
							"recommended": map[string]any{"type": "boolean"},
						},
						"required": []any{"label"},
					},
				},
			},
			"required": []any{"question", "options"},
		},
	}
}

// Run surfaces the question and blocks until the user answers. Returns the
// chosen label (or custom text) so the model can continue with the decision.
func (t *Tool) Run(ctx context.Context, in map[string]any) (tools.Result, error) {
	q := ask.Question{
		Prompt:      str(in["question"]),
		Header:      str(in["header"]),
		Options:     parseOptions(in["options"]),
		AllowCustom: true,
	}
	if q.Prompt == "" || len(q.Options) == 0 {
		return tools.Result{Content: "ask_user needs a question and at least one option", IsError: true}, nil
	}

	ans, err := t.asker.Ask(ctx, q)
	if err != nil {
		return tools.Result{Content: "ask_user: " + err.Error(), IsError: true}, nil
	}
	if ans.Dismissed {
		return tools.Result{Content: "User dismissed the question without choosing. Proceed with your best judgement or ask again."}, nil
	}
	if ans.Index >= 0 && ans.Index < len(q.Options) {
		return tools.Result{Content: fmt.Sprintf("User selected: %s", q.Options[ans.Index].Label)}, nil
	}
	return tools.Result{Content: fmt.Sprintf("User answered (custom): %s", ans.Text)}, nil
}

// parseOptions decodes the JSON options array (each item a map[string]any).
func parseOptions(v any) []ask.Option {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]ask.Option, 0, len(arr))
	for _, it := range arr {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		label := str(m["label"])
		if label == "" {
			continue
		}
		rec, _ := m["recommended"].(bool)
		out = append(out, ask.Option{
			Label:       label,
			Description: str(m["description"]),
			Recommended: rec,
		})
	}
	return out
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
