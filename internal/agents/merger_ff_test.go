//go:build !windows

package agents

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
)

// main parked in a second worktree makes every fast-forward fail; the gate must
// not re-run forever
func TestPersistentFFFailureBlocksAfterHopBudget(t *testing.T) {
	f := newFixture(t)
	git(t, f.repo, "switch", "-q", "-c", "other")
	git(t, f.repo, "worktree", "add", "-q", filepath.Join(t.TempDir(), "mainwt"), "main")
	counter := filepath.Join(t.TempDir(), "runs")
	sw := config.SwarmConfig{Verify: []string{"echo x >> " + counter}}
	for i := 0; i < 10; i++ {
		scan(context.Background(), f.repo, "", sw)
	}
	b, _ := os.ReadFile(counter)
	s := f.status(t)
	if s.MergeState != bgreg.MergeStateBlocked || s.State != bgreg.StateFailed {
		t.Fatalf("want blocked after repeated ff failures, got state=%s merge=%s", s.State, s.MergeState)
	}
	if !strings.Contains(s.ConflictMsg, "ff-merge failed") {
		t.Fatalf("block reason lost the cause: %q", s.ConflictMsg)
	}
	if runs := strings.Count(string(b), "x"); runs != sw.Hops()+1 {
		t.Fatalf("verify ran %d times, want %d", runs, sw.Hops()+1)
	}
	if len(f.starts) != 0 {
		t.Fatalf("an environment failure must not restart the agent: %q", f.starts)
	}
}
