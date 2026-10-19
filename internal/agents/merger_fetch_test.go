//go:build !windows

package agents

import (
	"context"
	"os/exec"
	"testing"

	"github.com/elhenro/bee/internal/config"
)

// the rebase targets the local base, so a remote fetch under the merge lock buys
// nothing and can stall on credentials
func TestTryMergeDoesNotFetch(t *testing.T) {
	f := newFixture(t)
	remote := t.TempDir()
	git(t, remote, "init", "-q", "--bare", "-b", "main")
	git(t, f.repo, "remote", "add", "origin", remote)
	git(t, f.repo, "push", "-q", "origin", "main")
	git(t, f.repo, "update-ref", "-d", "refs/remotes/origin/main")

	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{})
	if !res.OK {
		t.Fatalf("res=%+v", res)
	}
	if err := exec.Command("git", "-C", f.repo, "rev-parse", "--verify", "-q", "refs/remotes/origin/main").Run(); err == nil {
		t.Fatal("TryMerge fetched from origin")
	}
}
