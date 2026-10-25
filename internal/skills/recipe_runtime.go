package skills

import (
	"fmt"
	"regexp"
	"strings"
)

// runtimeKinds validates the runtime fields of a step list. It reports
// runtime=true when every step carries a Kind, false when none does, and an
// error when the list mixes the two or a kinded step is malformed.
func runtimeKinds(steps []RecipeStep, ids map[string]bool) (bool, error) {
	kinded := 0
	for _, st := range steps {
		if st.Kind != "" {
			kinded++
		}
	}
	if kinded == 0 {
		return false, nil
	}
	if kinded != len(steps) {
		return false, fmt.Errorf("runtime recipe: every step needs a kind (tool|shell|bee), %d of %d have one", kinded, len(steps))
	}
	for _, st := range steps {
		if err := validateRuntimeStep(st, ids); err != nil {
			return false, fmt.Errorf("step %s: %w", st.ID, err)
		}
	}
	return true, nil
}

func validateRuntimeStep(st RecipeStep, ids map[string]bool) error {
	switch st.Kind {
	case StepTool:
		if st.Tool == "" {
			return fmt.Errorf("kind tool needs tool")
		}
		// a bash call must go through kind=shell so its template values are
		// shell-quoted; raw substitution into a command is an injection path.
		if st.Tool == "bash" || st.Tool == "escalate" {
			return fmt.Errorf("kind tool cannot name %q (use kind: shell, or next: escalate)", st.Tool)
		}
	case StepShell:
		if st.Run == "" {
			return fmt.Errorf("kind shell needs run")
		}
		if placeholderInQuotes(st.Run) {
			return fmt.Errorf("run: placeholder inside quotes; substitution quotes it already, write it bare")
		}
		if placeholderInUnsafeSyntax(st.Run) {
			return fmt.Errorf("run: templated runs may not use heredocs, comments, $(...), backticks, ${...}, unbalanced quotes, or a placeholder glued to other text; pass values as standalone arguments or env vars")
		}
	case StepBee:
		if st.Prompt == "" {
			return fmt.Errorf("kind bee needs prompt")
		}
	default:
		return fmt.Errorf("unknown kind %q (want tool|shell|bee)", st.Kind)
	}
	if st.Next != "" && st.Next != "stop" && st.Next != "escalate" && !ids[st.Next] {
		return fmt.Errorf("next %q is not a known step id", st.Next)
	}
	if st.FailIf != "" {
		if _, err := regexp.Compile(st.FailIf); err != nil {
			return fmt.Errorf("fail_if: %w", err)
		}
	}
	return nil
}

// renderRuntimeBody is what a model sees when a runtime recipe is folded into
// a prompt (TUI slash command). It cannot walk the steps itself, so it is told
// to invoke the recipe as a subcommand.
func renderRuntimeBody(s Skill) string {
	var b strings.Builder
	b.WriteString(bodyHead("Runtime recipe", s))
	b.WriteString("This recipe executes in Go, not by you. Run it with the bash tool: `bee " + s.Name + " [args...]` and report its output.\nSteps:\n")
	for i, st := range s.Steps {
		fmt.Fprintf(&b, "%d. [%s] (%s) %s\n", i+1, st.ID, st.Kind, st.Description)
	}
	return b.String()
}
