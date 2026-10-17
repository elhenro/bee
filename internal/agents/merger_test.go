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

const testID = "11111111-2222-3333-4444-555555555555"

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "change "+name)
}

// fixture is a main repo plus one DONE agent with a commit on its branch.
// Agent starts are recorded, never run. The recorded pid is this test
// process, so the merger sees the resumed agent as alive.
type fixture struct {
	repo, wt string
	starts   []string // last argv element (the prompt) of each start
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("BEE_HOME", t.TempDir())
	f := &fixture{repo: t.TempDir()}
	git(t, f.repo, "init", "-q", "-b", "main")
	git(t, f.repo, "config", "commit.gpgsign", "false")
	git(t, f.repo, "config", "user.name", "agents-test")
	git(t, f.repo, "config", "user.email", "agents@test")
	commit(t, f.repo, "shared.txt", "base\n")

	wt, err := WorktreePath(testID)
	if err != nil {
		t.Fatal(err)
	}
	f.wt = wt
	git(t, f.repo, "worktree", "add", "-q", "-b", "agents/test", wt)
	commit(t, wt, "feature.txt", "feature\n")
	_ = bgreg.Write(bgreg.Status{
		SessionID: testID, State: bgreg.StateDone, Task: "add feature",
		WorktreePath: wt, Branch: "agents/test", MergeState: bgreg.MergeStateUnmerged,
	})

	old := startAgent
	t.Cleanup(func() { startAgent = old })
	startAgent = func(cmd *exec.Cmd) (int, error) {
		f.starts = append(f.starts, cmd.Args[len(cmd.Args)-1])
		return os.Getpid(), nil
	}
	return f
}

func (f *fixture) status(t *testing.T) bgreg.Status {
	t.Helper()
	s, err := bgreg.Read(testID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *fixture) set(t *testing.T, fn func(*bgreg.Status)) {
	t.Helper()
	if err := bgreg.Update(testID, func(s *bgreg.Status) error { fn(s); return nil }); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) conflict(t *testing.T) {
	t.Helper()
	commit(t, f.repo, "shared.txt", "main side\n")
	commit(t, f.wt, "shared.txt", "agent side\n")
}

func TestConflictResumesOnceWithoutInboxSpam(t *testing.T) {
	f := newFixture(t)
	f.conflict(t)
	sw := config.SwarmConfig{}
	scan(context.Background(), f.repo, "", sw)
	scan(context.Background(), f.repo, "", sw)

	if len(f.starts) != 1 || !strings.Contains(f.starts[0], "could not rebase") || !strings.Contains(f.starts[0], "- shared.txt") {
		t.Fatalf("want one resume with the conflict, got %d: %q", len(f.starts), f.starts)
	}
	if msgs, _, _ := bgreg.InboxDrain(testID, 0); len(msgs) != 0 {
		t.Fatalf("conflict must not post to the inbox of an exited agent, got %d messages", len(msgs))
	}
	s := f.status(t)
	if s.State != bgreg.StateActive || s.Hops != 0 || s.Resumes != 1 {
		t.Fatalf("state=%s hops=%d resumes=%d, want active, no hop, one resume", s.State, s.Hops, s.Resumes)
	}
	if !strings.Contains(f.starts[0], "add feature") {
		t.Fatal("resume prompt must restate the task, the new process has no history")
	}
}

func TestRedVerifyCostsHopAndResumes(t *testing.T) {
	f := newFixture(t)
	mainBefore := git(t, f.repo, "rev-parse", "main")
	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{"echo broken; false"}})
	if !res.Red || res.OK {
		t.Fatalf("res=%+v, want red", res)
	}
	s := f.status(t)
	if s.Hops != 1 || s.Resumes != 1 || len(f.starts) != 1 || !strings.Contains(f.starts[0], "broken") {
		t.Fatalf("hops=%d resumes=%d starts=%q", s.Hops, s.Resumes, f.starts)
	}
	if git(t, f.repo, "rev-parse", "main") != mainBefore {
		t.Fatal("red verify must not advance main")
	}
}

func TestHopCapBlocks(t *testing.T) {
	f := newFixture(t)
	f.set(t, func(s *bgreg.Status) { s.Hops = 3 })
	TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{"false"}})
	s := f.status(t)
	if s.MergeState != bgreg.MergeStateBlocked || s.State != bgreg.StateFailed || len(f.starts) != 0 {
		t.Fatalf("merge=%s state=%s starts=%d, want blocked without resume", s.MergeState, s.State, len(f.starts))
	}
	if mergeable(s) {
		t.Fatal("blocked agent must not be retried")
	}
}

func TestResumeCapBlocks(t *testing.T) {
	f := newFixture(t)
	f.conflict(t)
	f.set(t, func(s *bgreg.Status) { s.Resumes = 3 })
	TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{})
	s := f.status(t)
	if s.MergeState != bgreg.MergeStateBlocked || len(f.starts) != 0 || !strings.Contains(s.ConflictMsg, "resume budget") {
		t.Fatalf("merge=%s starts=%d msg=%q", s.MergeState, len(f.starts), s.ConflictMsg)
	}
}

func TestGreenVerifyMerges(t *testing.T) {
	f := newFixture(t)
	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{"test -f feature.txt"}})
	if !res.OK {
		t.Fatalf("res=%+v", res)
	}
	if git(t, f.repo, "rev-parse", "main") != git(t, f.wt, "rev-parse", "HEAD") {
		t.Fatal("main not fast-forwarded to the agent branch")
	}
	if s := f.status(t); s.MergeState != bgreg.MergeStateMerged {
		t.Fatalf("merge state %s", s.MergeState)
	}
}

func TestRepoGateFileWins(t *testing.T) {
	f := newFixture(t)
	_ = os.MkdirAll(filepath.Join(f.repo, ".bee"), 0o755)
	if err := os.WriteFile(filepath.Join(f.repo, ".bee", "verify"), []byte("$ false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{"true"}})
	if !res.Red {
		t.Fatalf("repo .bee/verify must override the config fallback: %+v", res)
	}
}

func TestHeadMovedDuringVerifyIsFreeBounce(t *testing.T) {
	f := newFixture(t)
	mainBefore := git(t, f.repo, "rev-parse", "main")
	sneak := "git -C " + f.wt + " -c user.name=x -c user.email=x@x commit --allow-empty -q -m sneaky"
	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{sneak}})
	if res.OK {
		t.Fatal("a commit nobody verified must not merge")
	}
	s := f.status(t)
	if s.Hops != 0 || len(f.starts) != 0 || s.MergeState != bgreg.MergeStateUnmerged {
		t.Fatalf("hops=%d starts=%d merge=%s, want free bounce left for the next tick", s.Hops, len(f.starts), s.MergeState)
	}
	if git(t, f.repo, "rev-parse", "main") != mainBefore || !mergeable(s) {
		t.Fatal("main moved or agent not retried on the next tick")
	}
}

func TestCrashedAgentResumes(t *testing.T) {
	f := newFixture(t)
	dead := exec.Command("true")
	if err := dead.Run(); err != nil {
		t.Fatal(err)
	}
	f.set(t, func(s *bgreg.Status) { s.State = bgreg.StateActive; s.PID = dead.Process.Pid })
	scan(context.Background(), f.repo, "", config.SwarmConfig{})
	if len(f.starts) != 1 || !strings.Contains(f.starts[0], "stopped before it finished") {
		t.Fatalf("starts=%q", f.starts)
	}
	if s := f.status(t); s.Resumes != 1 || s.PID != os.Getpid() {
		t.Fatalf("resumes=%d pid=%d", s.Resumes, s.PID)
	}
}

func TestMergeableRequiresDone(t *testing.T) {
	base := bgreg.Status{Branch: "b", WorktreePath: "/w", MergeState: bgreg.MergeStateConflict}
	for _, st := range []bgreg.State{bgreg.StateActive, bgreg.StateAwaiting, bgreg.StateFailed} {
		base.State = st
		if mergeable(base) {
			t.Fatalf("%s agent in conflict must not be merged", st)
		}
	}
	base.State = bgreg.StateDone
	if !mergeable(base) {
		t.Fatal("done agent must be mergeable")
	}
}

func TestUntrackedFilesBounceWithoutHop(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.wt, "makes_tests_pass.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{"true"}})
	s := f.status(t)
	if res.OK || s.Hops != 0 || len(f.starts) != 1 || !strings.Contains(f.starts[0], "untracked") {
		t.Fatalf("ok=%v hops=%d starts=%v", res.OK, s.Hops, f.starts)
	}
}

func TestBranchEditingGateBlocks(t *testing.T) {
	f := newFixture(t)
	_ = os.MkdirAll(filepath.Join(f.wt, ".bee"), 0o755)
	commit(t, f.wt, ".bee/verify", "true\n")
	mainBefore := git(t, f.repo, "rev-parse", "main")
	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{"true"}})
	s := f.status(t)
	if res.OK || s.MergeState != bgreg.MergeStateBlocked || len(f.starts) != 0 {
		t.Fatalf("ok=%v merge=%s starts=%d", res.OK, s.MergeState, len(f.starts))
	}
	if git(t, f.repo, "rev-parse", "main") != mainBefore {
		t.Fatal("a gate edit must not reach main without a human")
	}
}

func TestGateCaseVariantAndSymlinkBlock(t *testing.T) {
	for name, edit := range map[string]func(t *testing.T, wt string){
		"case variant": func(t *testing.T, wt string) {
			_ = os.MkdirAll(filepath.Join(wt, ".BEE"), 0o755)
			commit(t, wt, ".BEE/verify", "true\n")
		},
		"symlinked dir": func(t *testing.T, wt string) {
			_ = os.MkdirAll(filepath.Join(wt, "elsewhere"), 0o755)
			commit(t, wt, "elsewhere/verify", "true\n")
			if err := os.Symlink("elsewhere", filepath.Join(wt, ".bee")); err != nil {
				t.Fatal(err)
			}
			git(t, wt, "add", ".bee")
			git(t, wt, "commit", "-q", "-m", "link")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			edit(t, f.wt)
			TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{"true"}})
			if s := f.status(t); s.MergeState != bgreg.MergeStateBlocked {
				t.Fatalf("merge=%s, a gate redirect must block for a human", s.MergeState)
			}
		})
	}
}

// verify sees the commit, not ignored leftovers in the agent's worktree.
func TestVerifyIgnoresIgnoredLeftovers(t *testing.T) {
	f := newFixture(t)
	commit(t, f.wt, ".gitignore", "gen.txt\n")
	if err := os.WriteFile(filepath.Join(f.wt, "gen.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{Verify: []string{"test -f gen.txt"}})
	if res.OK || !res.Red {
		t.Fatalf("an ignored file in the worktree must not make verify pass: %+v", res)
	}
	if out := git(t, f.repo, "worktree", "list"); strings.Count(out, "\n") != 1 {
		t.Fatalf("verify checkout not cleaned up:\n%s", out)
	}
}

// a symlinked gate could point at a file any branch may edit, so the merger
// refuses to judge against it until a human makes it a regular file.
func TestSymlinkedGateBlocks(t *testing.T) {
	for name, setup := range map[string]func(t *testing.T, repo string){
		"file link": func(t *testing.T, repo string) {
			commit(t, repo, "real", "true\n")
			_ = os.MkdirAll(filepath.Join(repo, ".bee"), 0o755)
			if err := os.Symlink("../real", filepath.Join(repo, ".bee", "verify")); err != nil {
				t.Fatal(err)
			}
		},
		"dir link": func(t *testing.T, repo string) {
			_ = os.MkdirAll(filepath.Join(repo, "cfg"), 0o755)
			commit(t, repo, "cfg/verify", "true\n")
			if err := os.Symlink("cfg", filepath.Join(repo, ".bee")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			setup(t, f.repo)
			mainBefore := git(t, f.repo, "rev-parse", "main")
			TryMerge(context.Background(), f.repo, f.status(t), config.SwarmConfig{})
			s := f.status(t)
			if s.MergeState != bgreg.MergeStateBlocked || !strings.Contains(s.ConflictMsg, "symlink") {
				t.Fatalf("merge=%s msg=%q", s.MergeState, s.ConflictMsg)
			}
			if git(t, f.repo, "rev-parse", "main") != mainBefore {
				t.Fatal("main moved past a symlinked gate")
			}
		})
	}
}
