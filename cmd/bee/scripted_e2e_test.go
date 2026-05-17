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

// TestScriptedHeadlessSingleTurn runs bee headless against a one-scenario
// fixture and asserts the scripted text appears in stdout.
func TestScriptedHeadlessSingleTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	bin := buildBee(t)
	home, sessDir := tmpHomeSessions(t)

	fixture := absFixture(t, "simple_text.json")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "--headless", "hello bee")
	cmd.Env = scriptedEnv(home, sessDir, fixture)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bee run failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "scripted: hello back") {
		t.Errorf("expected scripted output, got:\n%s", out)
	}
	if !sessionWritten(t, sessDir) {
		t.Errorf("expected ≥1 session .jsonl in %s", sessDir)
	}
}

// TestScriptedHeadlessToolTurn drives a 2-turn flow: model calls `read`,
// loop dispatches the tool, then model emits final text. Asserts the file
// body actually reaches the model side (visible via the tool_result block
// the second scenario matched on with role:"tool").
func TestScriptedHeadlessToolTurn(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	bin := buildBee(t)
	home, sessDir := tmpHomeSessions(t)

	// stage a real file the read tool will load
	target := filepath.Join(t.TempDir(), "payload.txt")
	if err := os.WriteFile(target, []byte("fixture-body-marker"), 0o644); err != nil {
		t.Fatal(err)
	}

	// substitute FIXTURE_FILE placeholder into a working copy of the fixture
	raw, err := os.ReadFile(filepath.Join("testdata", "mock_scenarios", "tool_read_turn.json"))
	if err != nil {
		t.Fatal(err)
	}
	expanded := strings.ReplaceAll(string(raw), "FIXTURE_FILE", filepath.ToSlash(target))
	scriptPath := filepath.Join(t.TempDir(), "tool_read_turn.expanded.json")
	if err := os.WriteFile(scriptPath, []byte(expanded), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// prompt path must use forward slashes to match the FIXTURE_FILE
	// substitution above (which also uses filepath.ToSlash). without this
	// the scenario matcher rejects the request on Windows where the raw
	// target carries backslashes.
	cmd := exec.CommandContext(ctx, bin, "run", "--headless", "please read "+filepath.ToSlash(target))
	cmd.Env = scriptedEnv(home, sessDir, scriptPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bee run failed: %v\noutput:\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "Reading the file") {
		t.Errorf("missing first-turn text:\n%s", got)
	}
	if !strings.Contains(got, "Done. Saw the fixture body.") {
		t.Errorf("missing final-turn text — loop did not feed tool_result back:\n%s", got)
	}
	// session rollout should also be on disk
	if !sessionWritten(t, sessDir) {
		t.Errorf("expected session .jsonl in %s", sessDir)
	}
}

// TestScriptedFixtureExhausted asserts that calling the model more times
// than the fixture allows surfaces the exhaustion error (proof that the
// mock is fail-fast, not silently looping).
func TestScriptedFixtureExhausted(t *testing.T) {
	if testing.Short() {
		t.Skip("skip e2e under -short")
	}
	bin := buildBee(t)
	home, sessDir := tmpHomeSessions(t)

	// fixture that intentionally requests a tool but provides no follow-up,
	// so the loop hits the fixture-exhausted error after dispatching read.
	target := filepath.Join(t.TempDir(), "payload.txt")
	_ = os.WriteFile(target, []byte("body"), 0o644)

	exhaust := `{
	  "scenarios": [
	    {"name":"only-turn","match":{"any":true},
	     "events":[
	       {"type":"tool_use","tool":{"id":"toolu_r","name":"read","input":{"path":"` + filepath.ToSlash(target) + `"}}},
	       {"type":"done","stop_reason":"tool_use"}
	     ]}
	  ]
	}`
	scriptPath := filepath.Join(t.TempDir(), "exhaust.json")
	if err := os.WriteFile(scriptPath, []byte(exhaust), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "--headless", "go")
	cmd.Env = scriptedEnv(home, sessDir, scriptPath)
	out, _ := cmd.CombinedOutput()
	// loop should surface fixture exhaustion from the second Stream call
	if !strings.Contains(string(out), "exhausted") {
		t.Errorf("expected fixture-exhausted error, got:\n%s", out)
	}
}

func buildBee(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bee"+exeSuffix())
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}
	return bin
}

func tmpHomeSessions(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	sess := filepath.Join(root, "sessions")
	for _, d := range []string{home, sess} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, sess
}

func absFixture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "mock_scenarios", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func scriptedEnv(home, sessDir, fixture string) []string {
	return append(os.Environ(),
		"BEE_TEST_PROVIDER=scripted",
		"BEE_TEST_SCRIPT="+fixture,
		"HOME="+home,
		"BEE_HOME="+home,
		"BEE_SESSIONS_DIR="+sessDir,
		"BEE_SKILLS_DIR="+filepath.Join(home, "skills"),
		"BEE_BIN_DIR="+filepath.Join(home, "bin"),
	)
}

func sessionWritten(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			return true
		}
	}
	return false
}
