package commands

import "context"

// InitPrompt is the user-turn body /init submits. The TUI (app_slash.go)
// special-cases /init to feed this to the engine so the model scans the repo
// with its own file/shell tools and writes AGENTS.md. Run here is the
// headless fallback only.
const InitPrompt = `Scan this project and write an AGENTS.md file at the repo root documenting it for coding agents.

Investigate before writing — do not guess:
- read README, package manifests (go.mod, package.json, pyproject.toml, Cargo.toml, etc.), and any existing AGENTS.md / CLAUDE.md
- map the top-level directory layout and what each major dir holds
- find the real build, test, lint, and run commands (check Makefile, scripts, CI config, package scripts)
- note language/framework, entry points, and any non-obvious conventions or architecture decisions

Then write AGENTS.md with these sections (skip any that genuinely don't apply):
- short project description (what it is, what's distinctive)
- Commands: exact build / test / lint / run invocations, copy-pasteable
- Layout: top-level dirs and their purpose
- Conventions: code style, patterns, gotchas an agent must respect

Keep it concise and factual — every command must be one you verified exists. If AGENTS.md already exists, update it in place rather than discarding good content. Write the file directly; don't dump its contents in chat.`

// registerInit wires /init for palette discovery. The interactive behavior is
// special-cased in the TUI (app_slash.go), which submits InitPrompt as a user
// turn; this Run is the generic/headless fallback.
func registerInit(r *Registry) {
	r.Register(Command{
		Name:        "init",
		Description: "scan the project and write an AGENTS.md file",
		Run: func(_ context.Context, _ []string, _ Side) (string, error) {
			return "init scans the project and writes AGENTS.md; run it from the TUI", nil
		},
	})
}
