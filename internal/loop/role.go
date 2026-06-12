package loop

import (
	"context"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/types"
)

// classifyTimeout bounds the per-turn posture side-call. Without it the
// classifier inherits the full turn context (minutes on a slow provider) and a
// hung side-call would burn the turn before the user's request even runs. On
// timeout the call returns its safe default (act).
const classifyTimeout = 8 * time.Second

// Role gates engine behavior per Run. one axis the user cycles with shift+tab;
// each role bundles tool surface + reasoning budget + orchestration.
//
//	RoleWorker: full tool surface; a per-turn classifier picks read-only vs act
//	            so small models don't reflex into shell on a greeting. default.
//	RoleScout:  read-only research + web; proposes a plan, never mutates.
//	RoleQueen:  spawns a hive (decompose → workers → critic → synthesize).
//
// yolo (auto-approve dangerous shell) is a separate toggle, not a role.
type Role string

const (
	RoleWorker Role = "worker"
	RoleScout  Role = "scout"
	RoleQueen  Role = "queen"
)

// ParseRole normalises a string into a Role, tolerating the legacy mode names
// (plan/auto/edit/yolo/mastermind) so an un-migrated config still resolves
// sanely. Unknown → RoleWorker.
func ParseRole(s string) Role {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "scout", "plan":
		return RoleScout
	case "queen", "mastermind":
		return RoleQueen
	case "worker", "auto", "edit", "yolo", "":
		return RoleWorker
	default:
		return RoleWorker
	}
}

// RoleThinking is the reasoning budget baked into each role, applied when the
// user hasn't pinned an explicit Thinking override. worker stays cheap, scout
// digs, queen maxes out for the hive.
func RoleThinking(r Role) llm.Thinking {
	switch r {
	case RoleScout:
		return llm.ThinkingHigh
	case RoleQueen:
		return llm.ThinkingMax
	default:
		return llm.ThinkingAuto
	}
}

// readOnlyTools lists tools allowed on a read-only turn (scout, or a worker
// turn the classifier judged informational). discovery only; no shell, no
// mutators. knowledge_search is allowed so the agent can pull context.
var readOnlyTools = map[string]bool{
	"read":             true,
	"search":           true,
	"glob":             true,
	"ls":               true,
	"knowledge_search": true,
	// ask_user lets the agent resolve ambiguity with the user before writing
	// a plan instead of guessing defaults.
	"ask_user": true,
}

// scoutExtraTools widen the read-only surface for scout — research leans on the
// web. allowed on top of readOnlyTools only when the role is scout.
var scoutExtraTools = map[string]bool{
	"web_search": true,
	"web_fetch":  true,
}

// planOnlyTools are dropped on an act turn. ask_user only makes sense while the
// agent is gathering decisions for a plan; once acting it just acts.
var planOnlyTools = map[string]bool{
	"ask_user": true,
}

// filterToolSpecsForRole narrows the tool surface. read-only turns keep only
// the read-only whitelist (scout also keeps the web tools); act turns keep
// everything except plan-only tools.
func filterToolSpecsForRole(specs []llm.ToolSpec, role Role, readOnly bool) []llm.ToolSpec {
	if !readOnly {
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
		if readOnlyTools[s.Name] || (role == RoleScout && scoutExtraTools[s.Name]) {
			out = append(out, s)
		}
	}
	return out
}

// applySkillToolGrants re-adds plan-only tools named in grants that the role
// filter dropped. Only plan-only tools qualify — a skill can grant ask_user
// but can't force write/bash back into a read-only turn. Specs already present
// are left untouched; granted names absent from the registry are ignored.
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

// rolePromptPrefix is prepended to the assembled system prompt on a read-only
// turn. Empty on act turns. The wording differs by role:
//
//	scout  — research, then propose a plan the user can approve. The whole
//	         point of scout is to plan; the post-scout picker converts the
//	         plan into a build turn.
//	worker — read-only was forced by the per-turn classifier on what looked
//	         like a research question. Worker should not plan-and-stop; the
//	         next turn resumes acting with full tools. Tell it to gather
//	         info, not to write a finished plan.
//	queen  — never reaches this path in normal operation (TUI routes queen
//	         through the hive; headless run() falls back to a worker turn
//	         with readOnly=false). The branch is kept queen-labeled and
//	         shares scout's web-tool rules so a future caller that does
//	         land here doesn't ship a misleading header or silently drop
//	         them.
func rolePromptPrefix(role Role, readOnly bool) string {
	if !readOnly {
		return ""
	}
	var prefix string
	switch role {
	case RoleScout:
		prefix = "## READ-ONLY MODE (SCOUT)\n" +
			"You are in scout mode. Do NOT modify files, run shell commands, " +
			"or call any mutator tools (none are available this turn). Read, " +
			"search, and think. When a decision is the user's to make and " +
			"you can't infer it, call ask_user with concrete options (mark " +
			"your suggested pick recommended) instead of guessing. Reply " +
			"with a concrete, ordered plan the user can approve before any " +
			"edits run. End your reply with a one-line summary the user can " +
			"act on.\n"
	case RoleQueen:
		// queen should never reach this path (TUI routes queen through the
		// hive; headless run() prints a warning and falls back to a worker
		// turn where readOnly=false so this branch is skipped). Kept
		// queen-labeled and parallel to scout so a future code path that
		// does land here doesn't ship a misleading "SCOUT" header or
		// silently lose the web-tool rules the scout branch adds below.
		prefix = "## READ-ONLY MODE (QUEEN)\n" +
			"You are in queen read-only mode (defensive — this branch is " +
			"not on the normal queen path). Do NOT modify files, run shell " +
			"commands, or call any mutator tools (none are available this " +
			"turn). Read, search, and think. Reply with a concrete, ordered " +
			"plan the user can approve before any edits run.\n"
	default:
		// worker (or any other role that hit a read-only posture decision).
		prefix = "## READ-ONLY TURN (WORKER)\n" +
			"This turn has no mutator tools — read, search, and gather " +
			"information only. Do NOT produce a finished plan: the next " +
			"turn resumes acting with full tools. Keep the reply short — " +
			"what you found, what still needs to be checked. Don't " +
			"summarize or end with a one-liner; the loop continues.\n"
	}
	if role == RoleScout || role == RoleQueen {
		prefix += "TOOL RULES (hard):\n" +
			"- To find code, symbols, types, functions, files, or anything " +
			"that lives in this repo, use search (local grep), glob, ls, and " +
			"read. NEVER use web_search or web_fetch to locate local code — " +
			"e.g. finding where `compactDoneMsg` is defined is a `search` " +
			"call, not a web search.\n" +
			"- web_search/web_fetch are ONLY for information that cannot exist " +
			"in this repo: external library docs, third-party APIs, current " +
			"events. If a `site:` filter would point at your own repo, you " +
			"want `search` instead.\n" +
			"- If a local search returns nothing, refine the query or path — " +
			"do NOT fall back to the web for local code.\n"
	}
	return prefix
}

// classifyPosture runs a cheap side-LLM call against userText to decide whether
// the worker turn needs only reading (true) or acting (false). On any error or
// ambiguous response, returns false (act) — safer to give the model full tools
// than to silently strip mutators. Separate function so tests can stub it.
func classifyPosture(ctx context.Context, p llm.Provider, model, userText string) bool {
	if p == nil || strings.TrimSpace(userText) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, classifyTimeout)
	defer cancel()
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
		return false
	}
	var buf strings.Builder
	for ev := range ch {
		switch ev.Type {
		case llm.EventTextDelta:
			buf.WriteString(ev.Delta)
		case llm.EventError:
			if ev.Err != nil && buf.Len() == 0 {
				return false
			}
		}
	}
	return parseClassifyReadOnly(buf.String())
}

// classifySystem is the classifier prompt. Asks for one token: plan or edit.
// kept short to fit any model's context and stay cheap.
const classifySystem = `You classify a developer request into one of two modes:

- plan: research, explain, explore, discuss, design, review. Read-only.
- edit: write code, change files, run commands, fix bugs, refactor, build.

Reply with exactly one word: "plan" or "edit". No prose, no punctuation.`

// parseClassifyReadOnly extracts read-only vs act from raw model text. "plan"
// → read-only (true); anything ambiguous → act (false).
func parseClassifyReadOnly(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".\"' \n\t")
	return strings.HasPrefix(s, "plan")
}
