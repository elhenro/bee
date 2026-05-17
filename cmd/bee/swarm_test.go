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

// TestSwarmSmoke builds bee and runs `bee swarm "do a thing" --workers 2`
// with BEE_TEST_PROVIDER=stub. Asserts exit 0 and a synthesis section in stdout.
func TestSwarmSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skip swarm smoke under -short")
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
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "swarm", "--workers", "2", "do a thing")
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
		t.Fatalf("bee swarm failed: %v\noutput:\n%s", err, out)
	}
	s := string(out)
	if !strings.Contains(s, "Synthesis") {
		t.Errorf("expected '## Synthesis' header in stdout, got:\n%s", s)
	}
	if !strings.Contains(s, "stub:") {
		t.Errorf("expected stub provider response in output:\n%s", s)
	}
}
