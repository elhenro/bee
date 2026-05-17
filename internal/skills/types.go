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
