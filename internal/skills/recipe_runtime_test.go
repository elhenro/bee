package skills

import (
	"strings"
	"testing"
)

func TestParse_RuntimeRecipe(t *testing.T) {
	src := `---
name: ship
type: recipe
description: test then commit
steps:
  - id: test
    kind: shell
    description: run tests
    run: go test ./...
    on_failure: explain
    next: commit
  - id: explain
    kind: bee
    description: explain failure
    prompt: "why: {{test}}"
    fail_if: "^NO"
    next: escalate
  - id: commit
    kind: tool
    description: write note
    tool: write
    args: {path: NOTE, content: "{{args}}"}
---
`
	s, err := Parse("ship.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Runtime || len(s.Steps) != 3 {
		t.Fatalf("runtime=%v steps=%d", s.Runtime, len(s.Steps))
	}
	if s.Steps[0].Kind != StepShell || s.Steps[0].Run != "go test ./..." || s.Steps[0].Next != "commit" {
		t.Fatalf("step0: %+v", s.Steps[0])
	}
	if s.Steps[1].Kind != StepBee || s.Steps[1].FailIf != "^NO" {
		t.Fatalf("step1: %+v", s.Steps[1])
	}
	if !strings.Contains(s.Body, "bee ship") || strings.Contains(s.Body, "Follow these steps in order") {
		t.Fatalf("runtime body should point at the subcommand, got:\n%s", s.Body)
	}
}

func TestParse_RuntimeRecipeErrors(t *testing.T) {
	head := "---\nname: r\ntype: recipe\nsteps:\n"
	cases := map[string]string{
		"mixed kinds":   head + "  - id: a\n    kind: shell\n    description: a\n    run: ls\n  - id: b\n    description: legacy\n---\n",
		"tool is bash":  head + "  - id: a\n    kind: tool\n    description: a\n    tool: bash\n---\n",
		"shell no run":  head + "  - id: a\n    kind: shell\n    description: a\n---\n",
		"bee no prompt": head + "  - id: a\n    kind: bee\n    description: a\n---\n",
		"unknown kind":  head + "  - id: a\n    kind: magic\n    description: a\n---\n",
		"bad next":      head + "  - id: a\n    kind: shell\n    description: a\n    run: ls\n    next: zz\n---\n",
		"bad fail_if":   head + "  - id: a\n    kind: shell\n    description: a\n    run: ls\n    fail_if: '('\n---\n",
		"single-quoted": head + "  - id: a\n    kind: shell\n    description: a\n    run: echo '{{args}}'\n---\n",
		"double-quoted": head + "  - id: a\n    kind: shell\n    description: a\n    run: echo \"x {{args}}\"\n---\n",
	}
	for name, src := range cases {
		if _, err := Parse("r.md", []byte(src)); err == nil {
			t.Errorf("%s: expected parse error", name)
		}
	}
}

func TestParse_LegacyRecipeStaysPromptWalked(t *testing.T) {
	src := "---\nname: r\ntype: recipe\nsteps:\n  - id: a\n    description: do it\n    tool: bash\n---\n"
	s, err := Parse("r.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if s.Runtime || !strings.Contains(s.Body, "Follow these steps in order") {
		t.Fatalf("legacy recipe changed: runtime=%v body=%q", s.Runtime, s.Body)
	}
}

func TestPlaceholderInQuotes(t *testing.T) {
	cases := map[string]bool{
		"echo {{args}}":               false,
		"echo 'lit' {{args}}":         false,
		"echo \"it's\" {{args}}":      false,
		"echo \\\"{{args}}":           false,
		"echo '{{args}}'":             true,
		"echo \"{{args}}\"":           true,
		"echo 'a' 'b {{x}}'":          true,
		"grep -rn {{argv.0}} '{{s}}'": true,
	}
	for run, want := range cases {
		if got := placeholderInQuotes(run); got != want {
			t.Errorf("%q: got %v want %v", run, got, want)
		}
	}
}
