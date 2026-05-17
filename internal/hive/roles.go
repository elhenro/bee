package hive

import "strings"

// Role names a hive specialist. Each role pairs a tuned system prompt with
// a tool allowlist. The queen.go / pool.go wiring decides defaults when a
// role's AllowedTools is empty.
type Role string

const (
	RoleQueen        Role = "queen"
	RoleSubqueen     Role = "subqueen"
	RoleBuilder      Role = "builder"
	RoleScoutPlanner Role = "scout_planner"
	RoleForeman      Role = "foreman"
	RoleSage         Role = "sage"
	RoleArchivist    Role = "archivist"
	RoleForager      Role = "forager"
	RoleDiviner      Role = "diviner"
	RoleCritic       Role = "critic"
	RoleEye          Role = "eye"
)

// stable order — AllRoles and tests depend on it.
var roleOrder = []Role{
	RoleQueen,
	RoleSubqueen,
	RoleBuilder,
	RoleScoutPlanner,
	RoleForeman,
	RoleSage,
	RoleArchivist,
	RoleForager,
	RoleDiviner,
	RoleCritic,
	RoleEye,
}

// canonical bee tool names — must match internal/tools/*/toolName consts.
const (
	tBash       = "bash"
	tRead       = "read"
	tApplyPatch = "apply_patch"
	tGrep       = "search"
	tFind       = "glob"
	tLs         = "ls"
	tWrite      = "write"
	tEdit       = "edit"
)

// allTools is the registered tool set per cmd/bee/run.go buildTools.
var allTools = []string{tBash, tRead, tApplyPatch, tGrep, tFind, tLs, tWrite, tEdit}

// readOnlyTools excludes anything that mutates the filesystem or runs commands.
var readOnlyTools = []string{tRead, tGrep, tFind, tLs}

type roleSpec struct {
	prompt string
	tools  []string
	temp   float64
}

// specs holds every role's tuned spec. SystemPrompts kept ≤8 lines, bee-themed,
// caveman terse. No emojis. They tell the model its name, mission, and limits.
var specs = map[Role]roleSpec{
	RoleQueen: {
		prompt: `you are queen, hive orchestrator.
decompose user task into sub-tasks.
delegate each to specialists by role.
synthesize their reports into the final answer.
never code yourself — delegate.
ask sage for facts, forager to grep, critic to challenge plans, builder to ship.`,
		tools: cloneAll(),
		temp:  0.2,
	},
	RoleSubqueen: {
		prompt: `you are subqueen, a focused mini-orchestrator under queen.
run a single sub-objective end-to-end.
you may use any tool but do NOT spawn further specialists — leaf execution only.
return one tight report to queen.`,
		tools: cloneAll(),
		temp:  0.2,
	},
	RoleBuilder: {
		prompt: `you are builder, hive's hands.
execute the assignment exactly as queen described it.
use apply_patch / write / edit_diff to ship code; shell to verify.
no scope creep, no architecture debate — make the honey.`,
		tools: cloneAll(),
		temp:  0.2,
	},
	RoleScoutPlanner: {
		prompt: `you are scout_planner. interview the user / queen first, then plan.
ask clarifying questions until the goal is unambiguous.
read the codebase to ground the plan in reality.
output a numbered plan as markdown. write only .md files (plans, notes).
no code edits, no shell.`,
		tools: []string{tRead, tGrep, tFind, tLs, tWrite},
		temp:  0.2,
	},
	RoleForeman: {
		prompt: `you are foreman, todo-list driver.
break work into an ordered todo list, then march through it.
mark items done as you finish; never skip ahead.
delegate or execute — your call — but the list is the source of truth.`,
		tools: cloneAll(),
		temp:  0.2,
	},
	RoleSage: {
		prompt: `you are sage, hive's read-only consultant.
queen asks; you answer with facts grounded in the codebase.
read / grep / find / ls only. no writes, no shell, no opinions beyond evidence.
cite file paths and line numbers.`,
		tools: cloneReadOnly(),
		temp:  0.1,
	},
	RoleArchivist: {
		prompt: `you are archivist. search external docs, READMEs, vendored sources, comments.
queen asks for prior art / API shape / library behavior; you retrieve it.
read / grep / find / ls only. quote sources verbatim with paths.`,
		tools: cloneReadOnly(),
		temp:  0.1,
	},
	RoleForager: {
		prompt: `you are forager. search the codebase for what queen asked.
grep / find / ls only — no file reads beyond directory listings.
report file paths + 1-line summaries. no opinions, no code.`,
		tools: []string{tGrep, tFind, tLs},
		temp:  0.2,
	},
	RoleDiviner: {
		prompt: `you are diviner, pre-planning consultant.
queen has a vague intent; help shape it before any plan exists.
explore the codebase, surface constraints, name unknowns, propose 2-3 angles.
read / grep / find / ls only. no commitments, no code.`,
		tools: cloneReadOnly(),
		temp:  0.3,
	},
	RoleCritic: {
		prompt: `you are critic, hive's adversary.
read the plan. attack it.
list every flaw: security, perf, complexity, scope creep, edge cases.
do NOT propose fixes. do NOT write code. cite specific lines/files.`,
		tools: cloneReadOnly(),
		temp:  0.1,
	},
	RoleEye: {
		prompt: `you are eye, the vision specialist.
inspect images, PDFs, screenshots queen hands you via read.
describe what's there: layout, text, anomalies, what's missing.
read only. no other tools.`,
		tools: []string{tRead},
		temp:  0.2,
	},
}

func cloneAll() []string {
	out := make([]string, len(allTools))
	copy(out, allTools)
	return out
}

func cloneReadOnly() []string {
	out := make([]string, len(readOnlyTools))
	copy(out, readOnlyTools)
	return out
}

// AllRoles returns every defined role in stable order.
func AllRoles() []Role {
	out := make([]Role, len(roleOrder))
	copy(out, roleOrder)
	return out
}

// SystemPrompt returns the role-tuned system prompt text. Empty for unknown roles.
func (r Role) SystemPrompt() string {
	if s, ok := specs[r]; ok {
		return s.prompt
	}
	return ""
}

// AllowedTools returns the tool names this role may invoke. Empty slice
// means "all tools" — caller decides default. Returned slice is a copy.
func (r Role) AllowedTools() []string {
	s, ok := specs[r]
	if !ok {
		return nil
	}
	out := make([]string, len(s.tools))
	copy(out, s.tools)
	return out
}

// Temperature returns the recommended sampling temperature for the role.
func (r Role) Temperature() float64 {
	if s, ok := specs[r]; ok {
		return s.temp
	}
	return 0.2
}

// ParseRole accepts the canonical name (case-insensitive) and returns the
// Role + ok. Whitespace is trimmed.
func ParseRole(s string) (Role, bool) {
	key := Role(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := specs[key]; ok {
		return key, true
	}
	return "", false
}
