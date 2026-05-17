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

// TestFanSmoke builds bee, runs `bee fan --per=count --count=3 --task "say hi"`
// with BEE_TEST_PROVIDER=stub, asserts exit 0 and that the summary lists 3
// workers with stub output merged.
func TestFanSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skip smoke under -short")
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
	sessDir := filepath.Join(tmp, "sessions")
	for _, d := range []string{home, sessDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "fan",
		"--per=count", "--count=3", "--task", "say hi", "--max=2",
	)
	cmd.Env = append(os.Environ(),
		"BEE_TEST_PROVIDER=stub",
		"HOME="+home,
		"BEE_HOME="+home,
		"BEE_SESSIONS_DIR="+sessDir,
		"BEE_SKILLS_DIR="+filepath.Join(home, "skills"),
		"BEE_BIN_DIR="+filepath.Join(home, "bin"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bee fan failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)
	wants := []string{
		"3 ok, 0 failed",
		"bee-1",
		"bee-2",
		"bee-3",
		"## bee-1",
		"stub:",
	}
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("output missing %q\n--out--\n%s", w, s)
		}
	}

	// each worker should have produced its own session file.
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	var jsonl int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			jsonl++
		}
	}
	if jsonl < 3 {
		t.Errorf("expected ≥3 session files, got %d", jsonl)
	}
}

// TestFanPerFile runs against tmpdir files via the file mode.
func TestFanPerFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skip smoke under -short")
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

	// create a working tmpdir with two .txt files for fan to glob.
	workDir := filepath.Join(tmp, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"alpha.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(workDir, n), []byte("body"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	home := filepath.Join(tmp, "home")
	sessDir := filepath.Join(tmp, "sessions")
	for _, d := range []string{home, sessDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "fan",
		"--per=file", "--task", "look at", "*.txt",
	)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(),
		"BEE_TEST_PROVIDER=stub",
		"HOME="+home,
		"BEE_HOME="+home,
		"BEE_SESSIONS_DIR="+sessDir,
		"BEE_SKILLS_DIR="+filepath.Join(home, "skills"),
		"BEE_BIN_DIR="+filepath.Join(home, "bin"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bee fan failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)
	for _, w := range []string{"alpha.txt", "beta.txt", "2 ok, 0 failed"} {
		if !strings.Contains(s, w) {
			t.Errorf("output missing %q\n--out--\n%s", w, s)
		}
	}
}

// TestFanRequiresTask is a unit-level guard: missing --task → exit 2.
func TestFanRequiresTask(t *testing.T) {
	if testing.Short() {
		t.Skip("skip smoke under -short")
	}
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "bee"+exeSuffix())
	ctxB, cancelB := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelB()
	if err := exec.CommandContext(ctxB, "go", "build", "-o", bin, ".").Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "fan", "--per=count", "--count=1")
	cmd.Env = append(os.Environ(), "BEE_TEST_PROVIDER=stub")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit non-zero, got 0\nout:\n%s", out)
	}
	if !strings.Contains(string(out), "--task is required") {
		t.Errorf("missing helpful error: %s", out)
	}
}
