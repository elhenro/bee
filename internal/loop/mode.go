package loop

import (
	"context"
	"strings"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// Mode gates engine behavior per Run.
//
//	ModePlan: read-only tools, agent produces plan only, no edits.
//	ModeAuto: classifier picks plan|edit per turn (default).
//	ModeEdit: full tool surface, dangerous commands still prompt.
//	ModeYolo: full tool surface, approvable commands auto-approved (no prompt).
type Mode string

const (
	ModePlan Mode = "plan"
	ModeAuto Mode = "auto"
	ModeEdit Mode = "edit"
	ModeYolo Mode = "yolo"
)

// ParseMode normalises a string into a Mode. Unknown → ModeEdit.
func ParseMode(s string) Mode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "plan":
		return ModePlan
	case "auto":
		return ModeAuto
	case "yolo":
		return ModeYolo
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
	// ask_user lets the agent resolve ambiguity with the user before writing
	// a plan instead of guessing defaults.
	"ask_user": true,
}

// planOnlyTools are dropped outside plan mode. ask_user only makes sense while
// the agent is gathering decisions for a plan; in edit/auto the agent just
// acts, so it shouldn't surface a question picker mid-edit.
var planOnlyTools = map[string]bool{
	"ask_user": true,
}

// filterToolSpecsForMode narrows the tool surface per mode. Plan mode keeps
// only the read-only whitelist; edit/auto keep everything except plan-only
// tools. auto resolves to plan|edit *before* this runs, so mode is concrete.
func filterToolSpecsForMode(specs []llm.ToolSpec, mode Mode) []llm.ToolSpec {
	if mode != ModePlan {
		out := make([]llm.ToolSpec, 0, len(specs))
		for _, s := range specs {
			if !planOnlyTools[s.Name] {
				out = append(out, s)
			}
		}
		return out
	}
	out := make([]llm.ToolSpec, 0, len(specs))
	for _, s := range specs {
		if planSafeTools[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// applySkillToolGrants re-adds plan-only tools named in grants that the mode
// filter dropped. Only plan-only tools qualify — a skill can grant ask_user
// but can't force write/bash back into plan mode. Specs already present are
// left untouched; granted names absent from the registry are ignored.
func applySkillToolGrants(specs []llm.ToolSpec, reg *tools.Registry, grants []string) []llm.ToolSpec {
	if reg == nil || len(grants) == 0 {
		return specs
	}
	present := make(map[string]bool, len(specs))
	for _, s := range specs {
		present[s.Name] = true
	}
	for _, name := range grants {
		if !planOnlyTools[name] || present[name] {
			continue
		}
		if t, ok := reg.Get(name); ok {
			specs = append(specs, t.Spec())
			present[name] = true
		}
	}
	return specs
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
		"and think. When a decision is the user's to make and you can't infer " +
		"it, call ask_user with concrete options (mark your suggested pick " +
		"recommended) instead of guessing. Reply with a concrete, ordered plan " +
		"the user can approve before any edits run. End your reply with a " +
		"one-line summary the user can act on.\n"
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
