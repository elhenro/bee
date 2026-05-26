// Package skills defines skill types and the contract a skill executor satisfies.
//
// A Skill is a markdown file with YAML frontmatter. It can be: a prompt template
// (type=prompt), an external command (type=exec), an MCP server tool (type=mcp),
// or an HTTP endpoint (type=http). Each skill is surfaced two ways:
//  1. as `bee <name> [args...]` non-interactively
//  2. as a model-callable tool in the live agent session
package skills

import "context"

type Kind string

const (
	KindPrompt Kind = "prompt"
	KindExec   Kind = "exec"
	KindMCP    Kind = "mcp"
	KindHTTP   Kind = "http"
	// KindRecipe is a sequenced multi-step skill. Frontmatter declares an
	// ordered list of steps; bee renders them into a constrained prompt
	// addendum so small models that drop steps in free-form planning stay
	// on the rails — the next step is always the next line of the prompt.
	KindRecipe Kind = "recipe"
)

// Skill is the parsed result of one ~/.bee/skills/*.md file.
type Skill struct {
	Path        string
	Name        string
	Kind        Kind
	Description string
	Tools       []string // optional whitelist (prompt kind)
	Model       string   // optional override
	AutoApprove []string

	// kind=prompt
	Body string

	// kind=exec
	Exec   []string
	Stream bool

	// kind=mcp
	Server MCPServer

	// kind=http
	Endpoint string
	Auth     HTTPAuth

	// kind=recipe
	Steps []RecipeStep
}

// RecipeStep is one ordered step in a recipe skill. ID is optional (the
// parser auto-assigns step-N when empty) but useful when a later step's
// OnFailure jumps to a named recovery step.
type RecipeStep struct {
	ID          string
	Description string
	// Tool is the tool the model should call. Use "escalate" to ask the
	// user. Empty = free-form (model chooses for this step).
	Tool string
	// Args is an optional example payload rendered into the prompt as a
	// literal JSON envelope. Small models follow shape examples better
	// than schema prose.
	Args map[string]any
	// OnFailure: step id to jump to if Tool fails. "escalate" = call the
	// escalate tool. Empty = abort the recipe.
	OnFailure string
}

type MCPServer struct {
	Command string
	Args    []string
	Env     map[string]string
}

type HTTPAuth struct {
	Type   string // "bearer" | "header" | "none"
	Env    string // env var holding the secret
	Header string // header name (for type=header)
}

// Executor runs a skill non-interactively given the user message + ambient
// context, returning a single string result.
type Executor interface {
	Exec(ctx context.Context, s Skill, userMsg string) (string, error)
}
