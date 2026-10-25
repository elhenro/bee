package waggle

import (
	"fmt"
	"slices"
	"strings"

	yaml "github.com/goccy/go-yaml"

	"github.com/elhenro/bee/internal/safety"
)

// Scope decides where a waggle lives and how widely it applies.
type Scope string

const (
	ScopeProject Scope = "project" // routes that carry project paths (default)
	ScopeUser    Scope = "user"    // portable routes available across projects
)

// frontmatter is the YAML header of a crystallized waggle. The skills parser
// reads name/type/description/tools/exec/steps and ignores the rest; origin,
// scope, params, uses and yield are waggle metadata for lookup, replay and
// curation. An exec waggle carries tools/exec/route; a recipe waggle carries
// steps instead: it has no exec: vector, so skillexec never registers it as a
// model tool, and no route: key, so the replayer never fires it.
type frontmatter struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools,omitempty"`
	Origin      string   `yaml:"origin"`
	Scope       string   `yaml:"scope"`
	Params      []string `yaml:"params,omitempty"`
	Exec        []string `yaml:"exec,omitempty"`
	Uses        int      `yaml:"uses"`
	Yield       int      `yaml:"yield"`
	// Disabled is set by curation when a route's replay repeatedly diverged
	// without paying off. The file is kept for inspection but skipped by replay.
	Disabled bool `yaml:"disabled,omitempty"`
	// Route is the structured per-step form the replayer rehydrates: each step's
	// tool plus its args, with varying positions encoded as positional tokens
	// ($N) and fixed positions as literals. The skills parser ignores it; only
	// replay reads it. Kept beside exec (the runnable joined script) so a waggle
	// stays a valid exec-skill while also being matchable step-by-step.
	Route []routeStep  `yaml:"route,omitempty"`
	Steps []recipeStep `yaml:"steps,omitempty"`
}

// routeStep is one step of a waggle's structured route on disk. A param arg
// holds its "$N" token; a literal arg holds its value.
type routeStep struct {
	Tool string            `yaml:"tool"`
	Args map[string]string `yaml:"args,omitempty"`
}

// marshal renders a waggle file: the YAML header plus a one-line body.
func marshal(fm frontmatter, body string) (string, error) {
	y, err := yaml.Marshal(fm)
	if err != nil {
		return "", err
	}
	return "---\n" + string(y) + "---\n" + body + "\n", nil
}

// stepCommands renders a candidate's steps as shell command lines: the joined
// script (stable, for naming and dedup) and the per-step commands. tok renders
// the token for the 1-based parameter ordinal n. A bash step is reproduced
// verbatim only when rawBash (a recipe, whose steps pass the approval gate)
// and may not vary: a parameterized raw command has no safe shape. ok=false
// means a step has no safe shell translation.
func stepCommands(c Candidate, tok func(n int) string, rawBash bool) (script string, cmds []string, ok bool) {
	if len(c.Steps) == 0 {
		return "", nil, false
	}
	pos, _ := assignParams(c.Params)
	paramTok := func(step int, key string) string {
		if n, ok := pos[Param{Step: step, Key: key}]; ok {
			return tok(n)
		}
		return ""
	}
	for s, call := range c.Steps {
		var cmd string
		var ok bool
		if rawBash && call.Tool == "bash" {
			cmd = call.Args["command"]
			ok = cmd != "" && !slices.ContainsFunc(c.Params, func(p Param) bool { return p.Step == s })
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

// scriptOf renders a candidate's steps into a single bash script (steps joined
// by "; "). ok=false means a step has no safe shell translation. The script is
// stable for a given route, so callers can hash it for deterministic naming and
// dedup.
func scriptOf(c Candidate) (string, bool) {
	script, _, ok := stepCommands(c, func(n int) string { return fmt.Sprintf("\"$%d\"", n) }, false)
	return script, ok
}

// hasSecret reports whether a mined command carries a secret shape; such a
// route is never crystallized, since the script is stored and printed verbatim.
func hasSecret(script string) bool { return safety.Redact(script) != script }

// Render builds the waggle exec-skill markdown for a candidate. It returns
// ok=false when any step has no deterministic shell translation, so an
// un-crystallizable route is never written. Timestamps are left to the ledger.
func Render(name string, c Candidate, scope Scope) (string, bool) {
	script, ok := scriptOf(c)
	if !ok {
		return "", false
	}
	_, paramNames := assignParams(c.Params)
	md, err := marshal(frontmatter{
		Name:        name,
		Type:        "exec",
		Description: describe(c.Steps, paramNames),
		Tools:       []string{"bash"},
		Origin:      "waggle",
		Scope:       string(scope),
		Params:      paramNames,
		Exec:        []string{"bash", "-c", script},
		Route:       routeOf(c),
	}, "Crystallized read-only route. Auto-generated waggle.")
	return md, err == nil
}

// assignParams maps each varying (step,key) to a 1-based positional ordinal and
// builds the parameter name list ($1 = paramNames[0]). Duplicate keys across
// steps are disambiguated with a step suffix.
func assignParams(params []Param) (map[Param]int, []string) {
	pos := make(map[Param]int, len(params))
	var names []string
	used := map[string]bool{}
	for i, p := range params {
		pos[p] = i + 1
		nm := p.Key
		if used[nm] {
			nm = fmt.Sprintf("%s_%d", p.Key, p.Step)
		}
		used[nm] = true
		names = append(names, nm)
	}
	return pos, names
}

// routeOf renders a candidate's steps into the structured on-disk route. Varying
// argument positions become "$N" tokens (matching the joined script), so the
// replayer can tell a wildcard prefix slot from a literal one without parsing
// shell. Mirrors assignParams so token ordinals line up with the exec script.
func routeOf(c Candidate) []routeStep {
	pos, _ := assignParams(c.Params)
	steps := make([]routeStep, len(c.Steps))
	for s, call := range c.Steps {
		args := make(map[string]string, len(call.Args))
		for k, v := range call.Args {
			if n, ok := pos[Param{Step: s, Key: k}]; ok {
				v = fmt.Sprintf("$%d", n)
			}
			args[k] = v
		}
		steps[s] = routeStep{Tool: call.Tool, Args: args}
	}
	return steps
}

func describe(steps []Call, params []string) string {
	tools := make([]string, len(steps))
	for i, s := range steps {
		tools[i] = s.Tool
	}
	d := "waggle: " + strings.Join(tools, " -> ")
	if len(params) > 0 {
		d += " (" + strings.Join(params, ", ") + ")"
	}
	return d
}
