package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/waggle"
)

// TestBeeWaggleDispatch verifies crystallized routes resolve as subcommands:
// an exec waggle follows directly, a recipe waggle (mined from a mutating
// route) runs through the recipe executor with its shell steps.
func TestBeeWaggleDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skip waggle dispatch e2e under -short")
	}
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(tmp, "bee"+exeSuffix())
	{
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("go build: %v", err)
		}
	}
	home := filepath.Join(tmp, "home")
	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEE_HOME", home)
	store, err := waggle.ProjectStore(work)
	if err != nil {
		t.Fatal(err)
	}
	execMD, ok := waggle.Render("wag_exec", waggle.Candidate{Steps: []waggle.Call{
		{Tool: "ls", Args: map[string]string{"path": "."}},
	}}, waggle.ScopeProject)
	if !ok {
		t.Fatal("render exec")
	}
	recipeMD, ok := waggle.RenderRecipe("wag_recipe", waggle.Candidate{
		Steps: []waggle.Call{
			{Tool: "bash", Args: map[string]string{"command": "echo step-one"}, Mutates: true, Verified: true},
			{Tool: "bash", Args: map[string]string{"command": "echo step-two"}, Mutates: true, Verified: true},
		},
	}, waggle.ScopeProject)
	if !ok {
		t.Fatal("render recipe")
	}
	if err := store.Write("wag_exec", execMD); err != nil {
		t.Fatal(err)
	}
	if err := store.Write("wag_recipe", recipeMD); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"BEE_TEST_PROVIDER=stub",
		"HOME="+home,
		"BEE_HOME="+home,
		"BEE_SESSIONS_DIR="+filepath.Join(tmp, "sessions"),
		"BEE_SKILLS_DIR="+filepath.Join(home, "skills"),
	)
	run := func(args ...string) (string, string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Env = env
		cmd.Dir = work
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		return string(out), stderr.String(), err
	}

	out, stderr, err := run("wag_exec")
	if err != nil || !strings.Contains(out, "marker.txt") {
		t.Fatalf("exec waggle: err=%v\nstdout:\n%s\nstderr:\n%s", err, out, stderr)
	}
	out, stderr, err = run("wag_recipe")
	if err != nil || !strings.Contains(out, "step-two") {
		t.Fatalf("recipe waggle: err=%v\nstdout:\n%s\nstderr:\n%s", err, out, stderr)
	}
	if !strings.Contains(stderr, "> s1 (shell)") || !strings.Contains(stderr, "> s2 (shell)") {
		t.Fatalf("recipe progress missing:\n%s", stderr)
	}
	if _, _, err := run("wag_nope"); err == nil {
		t.Fatal("unknown waggle should fail")
	}
}
