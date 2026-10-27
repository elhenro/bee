//go:build !windows

package agents

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
)

func TestMergeUsesRecordedBase(t *testing.T) {
	f := newFixture(t)
	git(t, f.repo, "branch", "-m", "main", "trunk")
	f.set(t, func(s *bgreg.Status) { s.Base = "trunk" })
	tip := git(t, f.wt, "rev-parse", "HEAD")

	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{})
	if !res.OK {
		t.Fatalf("res=%+v, want merged into trunk", res)
	}
	if got := git(t, f.repo, "rev-parse", "trunk"); got != tip {
		t.Fatalf("trunk=%s, want agent tip %s", got, tip)
	}
}

func TestMergeFromFeatureBranchLeavesMainAlone(t *testing.T) {
	f := newFixture(t)
	mainBefore := git(t, f.repo, "rev-parse", "main")
	// the user branched off before spawning; the agent forked from there
	git(t, f.repo, "branch", "feat", "main")
	commit(t, f.wt, "more.txt", "more\n")
	f.set(t, func(s *bgreg.Status) { s.Base = "feat" })
	tip := git(t, f.wt, "rev-parse", "HEAD")

	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{})
	if !res.OK {
		t.Fatalf("res=%+v, want merged into feat", res)
	}
	if git(t, f.repo, "rev-parse", "feat") != tip {
		t.Fatal("feat must receive the agent commits")
	}
	if git(t, f.repo, "rev-parse", "main") != mainBefore {
		t.Fatal("main must not move for an agent based on feat")
	}
}

func TestMergeWithoutBaseFallsBackToMain(t *testing.T) {
	f := newFixture(t)
	if got := baseOf(f.status(t)); got != "main" {
		t.Fatalf("legacy sidecar base = %q, want main", got)
	}
	if res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{}); !res.OK {
		t.Fatalf("res=%+v, want merged into main", res)
	}
}

func TestSpawnRecordsBaseAndKeepsChildStatus(t *testing.T) {
	f := newFixture(t)
	git(t, f.repo, "switch", "-q", "-c", "release/1")

	// the child writes its first status before the spawner records the pid
	old := startAgent
	t.Cleanup(func() { startAgent = old })
	startAgent = func(cmd *exec.Cmd) (int, error) {
		id := cmd.Args[5]
		_ = bgreg.Update(id, func(s *bgreg.Status) error {
			s.PID, s.LastResponse = 4242, "child wrote this"
			return nil
		})
		return os.Getpid(), nil
	}
	res, err := Spawn(SpawnOpts{Prompt: "do it", RepoRoot: f.repo})
	if err != nil {
		t.Fatal(err)
	}
	st, err := bgreg.Read(res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Base != "release/1" {
		t.Errorf("Base = %q, want release/1", st.Base)
	}
	if st.LastResponse != "child wrote this" || st.PID != 4242 {
		t.Errorf("spawner clobbered the child's status: pid=%d resp=%q", st.PID, st.LastResponse)
	}
}
