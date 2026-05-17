package loop

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/types"
)

// Mode gates engine behavior per Run.
//
//	ModePlan: read-only tools, agent produces plan only, no edits.
//	ModeEdit: full tool surface (default).
//	ModeAuto: classifier picks plan|edit per turn.
type Mode string

const (
	ModePlan Mode = "plan"
	ModeAuto Mode = "auto"
	ModeEdit Mode = "edit"
)

// ParseMode normalises a string into a Mode. Unknown → ModeEdit.
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "plan":
		return ModePlan
	case "auto":
		return ModeAuto
	case "edit", "":
		return ModeEdit
	default:
		return ModeEdit
	}
}

// planSafeTools lists tools allowed in plan mode. read-only discovery only;
// no shell, no mutators. knowledge_search is allowed so the agent can pull
// context for its plan.
var planSafeTools = map[string]bool{
	"read":             true,
	"search":           true,
	"glob":             true,
	"ls":               true,
	"knowledge_search": true,
}

// filterToolSpecsForMode drops tools that wouldn't be safe in plan mode.
// edit/auto pass through unchanged — auto resolves to plan|edit *before*
// this function is called, so this only narrows when mode == ModePlan.
func filterToolSpecsForMode(specs []llm.ToolSpec, mode Mode) []llm.ToolSpec {
	if mode != ModePlan {
		return specs
	}
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, s := range specs {
		if planSafeTools[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// modePromptPrefix is prepended to the assembled system prompt when the
// active mode is ModePlan. Tells the model to research and propose a plan
// without touching files. Empty for ModeEdit (no override needed).
func modePromptPrefix(mode Mode) string {
	if mode != ModePlan {
		return ""
	}
	return "## PLAN MODE\n" +
		"You are in plan mode. Do NOT modify files, run shell commands, or " +
		"call any mutator tools (none are available this turn). Read, search, " +
		"and think. Reply with a concrete, ordered plan the user can approve " +
		"before any edits run. End your reply with a one-line summary the user " +
		"can act on.\n"
}

// ClassifyMode runs a cheap side-LLM call against userText to pick plan|edit.
// On any error or ambiguous response, falls back to ModeEdit. classifier is
// a separate function so callers can stub it in tests.
func ClassifyMode(ctx context.Context, p llm.Provider, model, userText string) Mode {
	if p == nil || strings.TrimSpace(userText) == "" {
		return ModeEdit
	}
	req := llm.Request{
		Model:  model,
		System: classifySystem,
		Messages: []types.Message{
			{Role: types.RoleUser, Content: []types.ContentBlock{
				{Type: types.BlockText, Text: userText},
			}},
		},
		MaxTokens:   8,
		Temperature: 0,
		Stream:      true,
	}
	ch, err := p.Stream(ctx, req)
	if err != nil {
		return ModeEdit
	}
	var buf strings.Builder
	for ev := range ch {
		switch ev.Type {
		case llm.EventTextDelta:
			buf.WriteString(ev.Delta)
		case llm.EventError:
			if ev.Err != nil && buf.Len() == 0 {
				return ModeEdit
			}
		}
	}
	return parseClassifyOutput(buf.String())
}

// classifySystem is the classifier prompt. Asks for one token: plan or edit.
// kept short to fit any model's context and stay cheap.
const classifySystem = `You classify a developer request into one of two modes:

- plan: research, explain, explore, discuss, design, review. Read-only.
- edit: write code, change files, run commands, fix bugs, refactor, build.

Reply with exactly one word: "plan" or "edit". No prose, no punctuation.`

// parseClassifyOutput extracts plan|edit from raw model text. Defaults to
// ModeEdit when the answer is ambiguous — safer to err toward giving the
// model full tools than to silently strip mutators.
func parseClassifyOutput(s string) Mode {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".\"' \n\t")
	if strings.HasPrefix(s, "plan") {
		return ModePlan
	}
	return ModeEdit
}
