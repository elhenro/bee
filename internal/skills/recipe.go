package skills

import (
	"encoding/json"
	"fmt"
	"strings"
)

// recipeStepRaw mirrors the frontmatter shape on disk so parse.go can unmarshal
// `steps:` cleanly. kept in the recipe file so parse.go stays kind-agnostic.
type recipeStepRaw struct {
	ID          string         `yaml:"id"`
	Description string         `yaml:"description"`
	Tool        string         `yaml:"tool"`
	Args        map[string]any `yaml:"args"`
	OnFailure   string         `yaml:"on_failure"`
}

// recipeBuild validates the raw step list and folds it into the Skill. Called
// from Parse() when kind=recipe. knownTools may be nil; when provided, every
// step's Tool must match a known name or be the special "escalate" sentinel
// or empty (free-form).
func recipeBuild(s *Skill, raw []recipeStepRaw, knownTools map[string]bool) error {
	if len(raw) == 0 {
		return fmt.Errorf("recipe skill has no steps")
	}
	ids := map[string]bool{}
	steps := make([]RecipeStep, 0, len(raw))
	for i, r := range raw {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			id = fmt.Sprintf("step-%d", i+1)
		}
		if ids[id] {
			return fmt.Errorf("step %d: duplicate id %q", i+1, id)
		}
		ids[id] = true
		if strings.TrimSpace(r.Description) == "" {
			return fmt.Errorf("step %d (%s): missing description", i+1, id)
		}
		// when knownTools is supplied, every non-empty, non-escalate Tool
		// must exist. catches typos at parse time rather than turn time.
		if knownTools != nil && r.Tool != "" && r.Tool != "escalate" {
			if !knownTools[r.Tool] {
				return fmt.Errorf("step %d (%s): tool %q is not registered", i+1, id, r.Tool)
			}
		}
		steps = append(steps, RecipeStep{
			ID:          id,
			Description: strings.TrimSpace(r.Description),
			Tool:        r.Tool,
			Args:        r.Args,
			OnFailure:   r.OnFailure,
		})
	}
	// validate OnFailure references resolve. allow empty + "escalate"
	// sentinel + any known step id.
	for _, st := range steps {
		of := strings.TrimSpace(st.OnFailure)
		if of == "" || of == "escalate" {
			continue
		}
		if !ids[of] {
			return fmt.Errorf("step %s: on_failure %q is not a known step id", st.ID, of)
		}
	}
	s.Steps = steps
	// fold the steps into Body so existing prompt-skill rendering picks the
	// recipe up without engine changes. recipes show as a numbered checklist
	// the model walks through; the explicit "if step N fails ..." line gives
	// small models a concrete recovery branch instead of free-forming.
	s.Body = renderRecipeBody(*s)
	return nil
}

// renderRecipeBody turns a parsed recipe into the prompt addendum the model
// sees. one block per step: number, description, tool example (if any),
// failure routing (if any).
func renderRecipeBody(s Skill) string {
	var b strings.Builder
	b.WriteString("Recipe: ")
	b.WriteString(s.Name)
	b.WriteString("\n\n")
	if s.Description != "" {
		b.WriteString(s.Description)
		b.WriteString("\n\n")
	}
	b.WriteString("Follow these steps in order. After each step, call the named tool. If a step fails, follow its on_failure rule. Stop only when every step completes or a step routes to escalate.\n\n")
	for i, st := range s.Steps {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, st.ID, st.Description)
		if st.Tool != "" {
			fmt.Fprintf(&b, "   tool: %s\n", st.Tool)
		}
		if len(st.Args) > 0 {
			if raw, err := json.Marshal(st.Args); err == nil {
				fmt.Fprintf(&b, "   args: %s\n", string(raw))
			}
		}
		if st.OnFailure != "" {
			fmt.Fprintf(&b, "   on_failure: %s\n", st.OnFailure)
		}
	}
	return b.String()
}
