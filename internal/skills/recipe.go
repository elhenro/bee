package skills

import (
	"encoding/json"
	"fmt"
	"strings"
)

// recipeBuild validates the parsed step list and folds it into the Skill.
// Called from Parse() when kind=recipe. The tool names are not checked against a
// registry here; the runtime catches unknown-tool calls via the tools.Get path.
func recipeBuild(s *Skill, steps []RecipeStep) error {
	if len(steps) == 0 {
		return fmt.Errorf("recipe skill has no steps")
	}
	ids := map[string]bool{}
	for i := range steps {
		st := &steps[i]
		if st.ID = strings.TrimSpace(st.ID); st.ID == "" {
			st.ID = fmt.Sprintf("step-%d", i+1)
		}
		if ids[st.ID] {
			return fmt.Errorf("step %d: duplicate id %q", i+1, st.ID)
		}
		ids[st.ID] = true
		if st.Description = strings.TrimSpace(st.Description); st.Description == "" {
			return fmt.Errorf("step %d (%s): missing description", i+1, st.ID)
		}
		st.OnFailure = strings.TrimSpace(st.OnFailure)
		st.Kind = StepKind(strings.ToLower(strings.TrimSpace(string(st.Kind))))
		st.Run = strings.TrimSpace(st.Run)
		st.Prompt = strings.TrimSpace(st.Prompt)
		st.Next = strings.TrimSpace(st.Next)
	}
	// validate OnFailure references resolve. allow empty + "escalate" and
	// "abort" sentinels + any known step id.
	for _, st := range steps {
		of := st.OnFailure
		if of == "" || of == "escalate" || of == "abort" {
			continue
		}
		if !ids[of] {
			return fmt.Errorf("step %s: on_failure %q is not a known step id", st.ID, of)
		}
	}
	s.Steps = steps
	// a recipe whose every step names a kind runs in Go; the model only sees
	// the bee steps. mixed lists are rejected so a typo never silently demotes
	// a deterministic step to a model-walked one.
	runtime, err := runtimeKinds(steps, ids)
	if err != nil {
		return err
	}
	if runtime {
		s.Runtime = true
		s.Body = renderRuntimeBody(*s)
		return nil
	}
	// fold the steps into Body so existing prompt-skill rendering picks the
	// recipe up without engine changes. recipes show as a numbered checklist
	// the model walks through; the explicit "if step N fails ..." line gives
	// small models a concrete recovery branch instead of free-forming.
	s.Body = renderRecipeBody(*s)
	return nil
}

// bodyHead is the title and description block both recipe bodies open with.
func bodyHead(title string, s Skill) string {
	head := title + ": " + s.Name + "\n\n"
	if s.Description != "" {
		head += s.Description + "\n\n"
	}
	return head
}

// renderRecipeBody turns a parsed recipe into the prompt addendum the model
// sees. one block per step: number, description, tool example (if any),
// failure routing (if any).
func renderRecipeBody(s Skill) string {
	var b strings.Builder
	b.WriteString(bodyHead("Recipe", s))
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
