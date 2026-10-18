//go:build !windows

package agents

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
)

func TestScanSkipsOtherReposSessions(t *testing.T) {
	f := newFixture(t)
	other := t.TempDir()
	git(t, other, "init", "-q", "-b", "main")
	git(t, other, "config", "user.name", "agents-test")
	git(t, other, "config", "user.email", "agents@test")
	commit(t, other, "x.txt", "x\n")

	scan(context.Background(), other, "", config.SwarmConfig{})
	if s := f.status(t); s.MergeState != bgreg.MergeStateUnmerged {
		t.Fatalf("foreign repo's merger touched the session: merge=%s", s.MergeState)
	}
	if git(t, other, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Fatal("foreign checkout moved")
	}
}

func TestMergeLeavesUsersBranchAlone(t *testing.T) {
	f := newFixture(t)
	git(t, f.repo, "switch", "-q", "-c", "wip")
	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{})
	if !res.OK {
		t.Fatalf("res=%+v", res)
	}
	if got := git(t, f.repo, "rev-parse", "--abbrev-ref", "HEAD"); got != "wip" {
		t.Fatalf("user checkout moved to %s", got)
	}
	if git(t, f.repo, "rev-parse", "main") != git(t, f.wt, "rev-parse", "HEAD") {
		t.Fatal("main not fast-forwarded")
	}
}

func TestMergeKeepsAgentsHalfDoneRebase(t *testing.T) {
	f := newFixture(t)
	f.conflict(t)
	// agent started the rebase itself and exited mid-resolution
	rb := exec.Command("git", "rebase", "main")
	rb.Dir = f.wt
	if out, _ := rb.CombinedOutput(); !strings.Contains(string(out), "CONFLICT") {
		t.Fatalf("setup: rebase did not conflict: %s", out)
	}
	manual := filepath.Join(f.wt, "shared.txt")
	if err := os.WriteFile(manual, []byte("resolved by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{})
	if !rebasing(f.wt) {
		t.Fatal("merger aborted a rebase it did not start")
	}
	if b, _ := os.ReadFile(manual); string(b) != "resolved by hand\n" {
		t.Fatalf("manual resolution lost: %q", b)
	}
}
