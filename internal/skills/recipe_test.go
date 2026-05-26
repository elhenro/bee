package skills

import (
	"strings"
	"testing"
)

const recipeYAML = `---
name: tidy
type: recipe
description: Quick tidy + commit
steps:
  - id: read
    description: Read the file.
    tool: read
    args:
      path: main.go
  - id: tidy
    description: Run go fmt.
    tool: bash
    args:
      command: "go fmt ./..."
    on_failure: escalate
---
`

func TestParse_Recipe(t *testing.T) {
	s, err := Parse("test.md", []byte(recipeYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Kind != KindRecipe {
		t.Fatalf("wrong kind: %q", s.Kind)
	}
	if len(s.Steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(s.Steps))
	}
	if s.Steps[0].ID != "read" || s.Steps[0].Tool != "read" {
		t.Errorf("step 1 mismatch: %+v", s.Steps[0])
	}
	if s.Steps[1].OnFailure != "escalate" {
		t.Errorf("step 2 OnFailure: %q", s.Steps[1].OnFailure)
	}
	// rendered body must include numbered checklist + the tool example.
	if !strings.Contains(s.Body, "1. [read] Read the file.") {
		t.Errorf("body missing step 1 prefix; got:\n%s", s.Body)
	}
	if !strings.Contains(s.Body, `"command":"go fmt ./..."`) {
		t.Errorf("body missing tool args example; got:\n%s", s.Body)
	}
}

func TestParse_RecipeRejectsEmptySteps(t *testing.T) {
	yml := `---
name: bad
type: recipe
description: empty
---
`
	if _, err := Parse("bad.md", []byte(yml)); err == nil {
		t.Fatalf("expected error for empty steps")
	}
}

func TestParse_RecipeRejectsDuplicateID(t *testing.T) {
	yml := `---
name: bad
type: recipe
steps:
  - id: dup
    description: first
    tool: read
  - id: dup
    description: second
    tool: read
---
`
	if _, err := Parse("bad.md", []byte(yml)); err == nil {
		t.Fatalf("expected error for duplicate step id")
	}
}

func TestParse_RecipeRejectsUnknownOnFailureRef(t *testing.T) {
	yml := `---
name: bad
type: recipe
steps:
  - id: step-1
    description: first
    tool: read
    on_failure: nonexistent
---
`
	if _, err := Parse("bad.md", []byte(yml)); err == nil {
		t.Fatalf("expected error for unknown on_failure ref")
	}
}

func TestParse_RecipeRequiresDescription(t *testing.T) {
	yml := `---
name: bad
type: recipe
steps:
  - id: step-1
    tool: read
---
`
	if _, err := Parse("bad.md", []byte(yml)); err == nil {
		t.Fatalf("expected error for missing step description")
	}
}

func TestParse_RecipeAutoAssignsStepID(t *testing.T) {
	yml := `---
name: ok
type: recipe
steps:
  - description: first
    tool: read
  - description: second
    tool: bash
---
`
	s, err := Parse("ok.md", []byte(yml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Steps[0].ID != "step-1" || s.Steps[1].ID != "step-2" {
		t.Errorf("auto ids wrong: %q %q", s.Steps[0].ID, s.Steps[1].ID)
	}
}
