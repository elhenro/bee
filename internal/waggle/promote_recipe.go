package waggle

import (
	"fmt"
	"strings"

	yaml "github.com/goccy/go-yaml"
)

// recipeFrontmatter is the YAML header of a recipe waggle: a runtime recipe
// (every step kinded) carrying the same origin/scope/uses/yield metadata as an
// exec waggle. It has no exec: vector, so skillexec never registers it as a
// model tool, and no route: key, so the replayer never fires it. The only way
// to run it is `bee <name>`, where each shell step passes the approval gate.
type recipeFrontmatter struct {
	Name        string       `yaml:"name"`
	Type        string       `yaml:"type"`
	Description string       `yaml:"description"`
	Origin      string       `yaml:"origin"`
	Scope       string       `yaml:"scope"`
	Params      []string     `yaml:"params,omitempty"`
	Uses        int          `yaml:"uses"`
	Yield       int          `yaml:"yield"`
	Steps       []recipeStep `yaml:"steps"`
}

type recipeStep struct {
	ID          string `yaml:"id"`
	Kind        string `yaml:"kind"`
	Description string `yaml:"description"`
	Run         string `yaml:"run"`
}

// hasMutator reports whether a route carries a state-changing step and so must
// crystallize as a recipe rather than a replayable exec waggle.
func hasMutator(steps []Call) bool {
	for _, c := range steps {
		if c.Mutates {
			return true
		}
	}
	return false
}

// recipeScriptOf renders a candidate's steps as recipe shell commands: the
// joined script (stable, for naming and dedup), the per-step commands, and ok.
// Varying arguments become {{argv.N}} placeholders, which the recipe runtime
// quotes on substitution. A shell step is reproduced verbatim and may not vary:
// a parameterized raw command has no safe shape.
func recipeScriptOf(c Candidate) (script string, cmds []string, ok bool) {
	if len(c.Steps) == 0 {
		return "", nil, false
	}
	paramPos, _ := assignParams(c.Params)
	paramTok := func(step int, key string) string {
		if m := paramPos[step]; m != nil {
			if n, ok := m[key]; ok {
				return fmt.Sprintf("{{argv.%d}}", n-1)
			}
		}
		return ""
	}
	for s, call := range c.Steps {
		var cmd string
		var ok bool
		if call.Tool == "bash" {
			cmd, ok = call.Args["command"], paramPos[s] == nil && call.Args["command"] != ""
		} else {
			cmd, ok = shellCommand(s, call, paramTok)
		}
		if !ok {
			return "", nil, false
		}
		cmds = append(cmds, cmd)
	}
	script = strings.Join(cmds, "; ")
	if hasSecret(script) {
		return "", nil, false
	}
	return script, cmds, true
}

// RenderRecipe builds the recipe-waggle markdown for a candidate with mutating
// steps. ok=false when a step cannot be reproduced as a shell command.
func RenderRecipe(name string, c Candidate, scope Scope) (string, bool) {
	_, cmds, ok := recipeScriptOf(c)
	if !ok {
		return "", false
	}
	_, paramNames := assignParams(c.Params)
	for i := range paramNames {
		paramNames[i] = fmt.Sprintf("argv.%d", i)
	}
	fm := recipeFrontmatter{
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
	y, err := yaml.Marshal(fm)
	if err != nil {
		return "", false
	}
	return "---\n" + string(y) + "---\nCrystallized route with shell steps. Auto-generated waggle: not replayed, not a model tool. Run as `bee " + name + "`; each step passes the approval gate.\n", true
}
