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

// TestBeeSkillDispatch verifies `bee <skill>` looks up the skill in
// ~/.bee/skills, then runs it through the headless engine. Uses the
// stub provider so no network is required.
func TestBeeSkillDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skip dispatcher e2e under -short")
	}
	tmp := t.TempDir()
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
	skillsDir := filepath.Join(home, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillBody := `---
name: hello
type: prompt
description: say hi
---
You greet the user warmly.`
	if err := os.WriteFile(filepath.Join(skillsDir, "hello.md"), []byte(skillBody), 0o644); err != nil {
		t.Fatal(err)
	}

	env := append(os.Environ(),
		"BEE_TEST_PROVIDER=stub",
		"HOME="+home,
		"BEE_HOME="+home,
		"BEE_SESSIONS_DIR="+filepath.Join(tmp, "sessions"),
		"BEE_SKILLS_DIR="+skillsDir,
	)

	// happy path: skill resolves, body becomes user msg, stub echoes it back
	{
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "hello")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bee hello: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "greet the user warmly") {
			t.Errorf("expected skill body in stub echo:\n%s", out)
		}
	}

	// unknown name → exit 2 + usage
	{
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "no-such-skill")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("expected non-zero exit for unknown skill: %s", out)
		}
		if !strings.Contains(string(out), "unknown command") {
			t.Errorf("expected 'unknown command' in output:\n%s", out)
		}
	}

	// reserved name (version) wins over any skill named the same. Even if
	// someone dropped ~/.bee/skills/version.md, `bee version` prints version.
	if err := os.WriteFile(filepath.Join(skillsDir, "version.md"), []byte(skillBody), 0o644); err != nil {
		t.Fatal(err)
	}
	{
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, "version")
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("bee version: %v\n%s", err, out)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(out)), "bee ") {
			t.Errorf("reserved name lost to skill: %s", out)
		}
	}
}
