package waggle

import (
	"fmt"
	"strings"

	yaml "github.com/goccy/go-yaml"
)

// Scope decides where a waggle lives and how widely it applies.
type Scope string

const (
	ScopeProject Scope = "project" // routes that carry project paths (default)
	ScopeUser    Scope = "user"    // portable routes available across projects
)

// frontmatter is the YAML header of a crystallized waggle. The skills parser
// reads name/type/description/tools/exec and ignores the rest; origin, scope,
// params, uses and yield are waggle metadata for lookup, replay and curation.
type frontmatter struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
	Origin      string   `yaml:"origin"`
	Scope       string   `yaml:"scope"`
	Params      []string `yaml:"params,omitempty"`
	Exec        []string `yaml:"exec"`
	Uses        int      `yaml:"uses"`
	Yield       int      `yaml:"yield"`
}

// Render builds the waggle exec-skill markdown for a candidate. It returns
// ok=false when any step has no deterministic shell translation, so an
// un-crystallizable route is never written. Timestamps are left to the ledger.
func Render(name string, c Candidate, scope Scope) (string, bool) {
	if len(c.Steps) == 0 {
		return "", false
	}
	paramPos, paramNames := assignParams(c.Params)
	paramTok := func(step int, key string) string {
		if m := paramPos[step]; m != nil {
			if n, ok := m[key]; ok {
				return fmt.Sprintf("$%d", n)
			}
		}
		return ""
	}
	cmds := make([]string, 0, len(c.Steps))
	for s, call := range c.Steps {
		cmd, ok := shellCommand(s, call, paramTok)
		if !ok {
			return "", false
		}
		cmds = append(cmds, cmd)
	}
	fm := frontmatter{
		Name:        name,
		Type:        "exec",
		Description: describe(c.Steps, paramNames),
		Tools:       []string{"bash"},
		Origin:      "waggle",
		Scope:       string(scope),
		Params:      paramNames,
		Exec:        []string{"bash", "-c", strings.Join(cmds, "; ")},
	}
	y, err := yaml.Marshal(fm)
	if err != nil {
		return "", false
	}
	return "---\n" + string(y) + "---\nCrystallized read-only route. Auto-generated waggle.\n", true
}

// assignParams maps each varying (step,key) to a 1-based positional ordinal and
// builds the parameter name list ($1 = paramNames[0]). Duplicate keys across
// steps are disambiguated with a step suffix.
func assignParams(params []Param) (map[int]map[string]int, []string) {
	pos := map[int]map[string]int{}
	var names []string
	used := map[string]bool{}
	for i, p := range params {
		if pos[p.Step] == nil {
			pos[p.Step] = map[string]int{}
		}
		pos[p.Step][p.Key] = i + 1
		nm := p.Key
		if used[nm] {
			nm = fmt.Sprintf("%s_%d", p.Key, p.Step)
		}
		used[nm] = true
		names = append(names, nm)
	}
	return pos, names
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
