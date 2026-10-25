package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBeeRuntimeRecipe verifies `bee <recipe>` executes a runtime recipe in
// Go: shell steps run without a model, a bee step gets the templated prompt
// (the stub provider echoes it), and the last step's output lands on stdout.
func TestBeeRuntimeRecipe(t *testing.T) {
	if testing.Short() {
		t.Skip("skip recipe e2e under -short")
	}
	tmp := t.TempDir()
	bin := buildBee(t)
	home := filepath.Join(tmp, "home")
	skillsDir := filepath.Join(home, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	recipe := `---
name: greet
type: recipe
description: shell then bee
steps:
  - id: who
    kind: shell
    description: name from args
    run: echo hello {{argv.0}}
  - id: say
    kind: bee
    description: let the model answer
    prompt: "repeat exactly: {{who}}"
---
`
	if err := os.WriteFile(filepath.Join(skillsDir, "greet.md"), []byte(recipe), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"BEE_TEST_PROVIDER=stub",
		"HOME="+home,
		"BEE_HOME="+home,
		"BEE_SESSIONS_DIR="+filepath.Join(tmp, "sessions"),
		"BEE_SKILLS_DIR="+skillsDir,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "greet", "world")
	cmd.Env = env
	cmd.Dir = tmp
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bee greet: %v\nstderr:\n%s", err, stderr.String())
	}
	// stdout is only the last step's output: the stub's echo of the prompt,
	// which carries the shell step's output through the template.
	if got := string(out); !strings.Contains(got, "stub: repeat exactly: hello world") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", got, stderr.String())
	}
	if !strings.Contains(stderr.String(), "> who (shell)") || !strings.Contains(stderr.String(), "> say (bee)") {
		t.Fatalf("progress log missing:\n%s", stderr.String())
	}
}
