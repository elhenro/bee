package waggle

import (
	"fmt"
	"slices"
)

type recipeStep struct {
	ID          string `yaml:"id"`
	Kind        string `yaml:"kind"`
	Description string `yaml:"description"`
	Run         string `yaml:"run"`
}

// hasMutator reports whether a route carries a state-changing step and so must
// crystallize as a recipe rather than a replayable exec waggle.
func hasMutator(steps []Call) bool {
	return slices.ContainsFunc(steps, func(c Call) bool { return c.Mutates })
}

// recipeCmds renders a candidate's steps as recipe shell commands: the joined
// script (stable, for naming and dedup), the per-step commands, and ok.
// Varying arguments become {{argv.N}} placeholders, which the recipe runtime
// quotes on substitution.
func recipeCmds(c Candidate) (script string, cmds []string, ok bool) {
	return stepCommands(c, func(n int) string { return fmt.Sprintf("{{argv.%d}}", n-1) }, true)
}

// RenderRecipe builds the recipe-waggle markdown for a candidate with mutating
// steps: a runtime recipe (every step kinded) carrying the same
// origin/scope/uses/yield metadata as an exec waggle. The only way to run it is
// `bee <name>`, where each shell step passes the approval gate. ok=false when a
// step cannot be reproduced as a shell command.
func RenderRecipe(name string, c Candidate, scope Scope) (string, bool) {
	_, cmds, ok := recipeCmds(c)
	if !ok {
		return "", false
	}
	_, paramNames := assignParams(c.Params)
	for i := range paramNames {
		paramNames[i] = fmt.Sprintf("argv.%d", i)
	}
	fm := frontmatter{
		Name:        name,
		Type:        "recipe",
		Description: describe(c.Steps, paramNames),
		Origin:      "waggle",
		Scope:       string(scope),
		Params:      paramNames,
	}
	for i, call := range c.Steps {
		fm.Steps = append(fm.Steps, recipeStep{
			ID:          fmt.Sprintf("s%d", i+1),
			Kind:        "shell",
			Description: call.Tool,
			Run:         cmds[i],
		})
	}
	md, err := marshal(fm, "Crystallized route with shell steps. Auto-generated waggle: not replayed, not a model tool. Run as `bee "+name+"`; each step passes the approval gate.")
	return md, err == nil
}
