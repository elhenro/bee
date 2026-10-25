// Package skills defines skill types and parses them from markdown files.
//
// A Skill is a markdown file with YAML frontmatter. It can be: a prompt template
// (type=prompt), an external command (type=exec), or an ordered multi-step
// recipe (type=recipe). Each skill is surfaced two ways:
//  1. as `bee <name> [args...]` non-interactively
//  2. as a model-callable tool in the live agent session (exec only; prompt
//     and recipe fold into the user turn)
package skills

type Kind string

const (
	KindPrompt Kind = "prompt"
	KindExec   Kind = "exec"
	// KindRecipe is a sequenced multi-step skill. Frontmatter declares an
	// ordered list of steps; bee renders them into a constrained prompt
	// addendum so small models that drop steps in free-form planning stay
	// on the rails — the next step is always the next line of the prompt.
	// No runtime enforcement: the model is asked to follow the order, not
	// forced to. Don't use it for safety-critical sequencing.
	KindRecipe Kind = "recipe"
)

// Skill is the parsed result of one ~/.bee/skills/*.md file.
type Skill struct {
	Path        string
	Name        string
	Kind        Kind
	Description string
	Tools       []string // optional whitelist (prompt kind)

	// kind=prompt
	Body string

	// kind=exec
	Exec   []string
	Stream bool

	// kind=recipe
	Steps []RecipeStep
	// Runtime is set when every recipe step carries a Kind: bee then executes
	// the steps in Go (tool/shell) and hands only bee steps to a model, instead
	// of folding the list into a prompt for the model to walk.
	Runtime bool
}

// StepKind selects how a runtime recipe step executes. Empty means the legacy
// prompt-walked step: the model reads the step and decides what to call.
type StepKind string

const (
	StepTool  StepKind = "tool"  // run a registered tool with literal args, no model
	StepShell StepKind = "shell" // run a command through the gated bash tool
	StepBee   StepKind = "bee"   // one bounded model turn driven by a prompt template
)

// RecipeStep is one ordered step in a recipe skill. ID is optional (the
// parser auto-assigns step-N when empty) but useful when a later step's
// OnFailure jumps to a named recovery step.
type RecipeStep struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	// Tool is the tool the model should call. Use "escalate" to ask the
	// user. Empty = free-form (model chooses for this step).
	Tool string `yaml:"tool"`
	// Args is an optional example payload rendered into the prompt as a
	// literal JSON envelope. Small models follow shape examples better
	// than schema prose.
	Args map[string]any `yaml:"args"`
	// OnFailure: step id to jump to if Tool fails. "escalate" = call the
	// escalate tool. Empty = abort the recipe.
	OnFailure string `yaml:"on_failure"`

	// runtime-only fields (Kind != ""). Templates may reference {{args}},
	// {{argv.N}} and {{<step-id>}} for an earlier step's output; inside Run
	// each value is shell-quoted on substitution.
	Kind   StepKind `yaml:"kind"`
	Run    string   `yaml:"run"`    // kind=shell: the command line
	Prompt string   `yaml:"prompt"` // kind=bee: the prompt template
	Tools  []string `yaml:"tools"`  // kind=bee: optional tool allowlist for that turn
	// Next routes after success: "" = following step, "stop" = finish ok,
	// "escalate" = finish and hand back to the user, or a step id.
	Next string `yaml:"next"`
	// FailIf is a regexp; a step whose output matches it counts as failed
	// even when the tool itself succeeded. Lets a bee step branch by answer.
	FailIf string `yaml:"fail_if"`
}
