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

// TestRunHeadlessSmoke builds bee, runs `bee run --headless "echo test"`
// with BEE_TEST_PROVIDER=stub, asserts exit 0 and a session file written.
func TestRunHeadlessSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skip smoke under -short")
	}
	tmp := t.TempDir()
	// build the binary
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
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "--headless", "echo test")
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
		t.Fatalf("bee run failed: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(string(out), "stub:") {
		t.Errorf("expected stub provider response in output:\n%s", out)
	}
	// at least one session file should now exist
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	var jsonl int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			jsonl++
		}
	}
	if jsonl < 1 {
		t.Errorf("expected ≥1 session .jsonl file, got %d (entries=%v)", jsonl, entries)
	}
}
