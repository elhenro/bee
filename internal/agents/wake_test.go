//go:build !windows

package agents

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
)

// a reply sent after the agent wrote DONE restarts it instead of merging it
func TestScanWakesDoneAgentWithUnreadReply(t *testing.T) {
	f := newFixture(t)
	mainBefore := git(t, f.repo, "rev-parse", "main")
	_ = bgreg.InboxAppend(testID, "also handle the empty case")

	scan(context.Background(), f.repo, "", config.SwarmConfig{})
	scan(context.Background(), f.repo, "", config.SwarmConfig{})

	if len(f.starts) != 1 || !strings.Contains(f.starts[0], "add feature") {
		t.Fatalf("want one start restating the task, got %q", f.starts)
	}
	s := f.status(t)
	if s.State != bgreg.StateActive || s.MergeState != bgreg.MergeStateUnmerged || s.Resumes != 0 {
		t.Fatalf("state=%s merge=%s resumes=%d, want active/unmerged with no budget spent", s.State, s.MergeState, s.Resumes)
	}
	if git(t, f.repo, "rev-parse", "main") != mainBefore {
		t.Fatal("agent was merged before it read the reply")
	}
	// the woken process owns the inbox now, a second wake must not fire
	if !bgreg.InboxPending(testID) {
		t.Fatal("wake must leave the reply unread for the new process")
	}
}

func TestWakeReachesBlockedAgent(t *testing.T) {
	f := newFixture(t)
	Block(testID, "needs a human")
	_ = bgreg.InboxAppend(testID, "use the v2 api")
	scan(context.Background(), f.repo, "", config.SwarmConfig{})
	if len(f.starts) != 1 {
		t.Fatalf("want a human reply to wake a blocked agent, got %d starts", len(f.starts))
	}
	if s := f.status(t); s.State != bgreg.StateActive || s.MergeState == bgreg.MergeStateBlocked || s.ConflictMsg != "" {
		t.Fatalf("blocked marks not cleared: %+v", s)
	}
}

func TestWakeIgnoresAgentWithoutReply(t *testing.T) {
	f := newFixture(t)
	if ok, err := Wake(f.repo, f.status(t)); ok || err != nil {
		t.Fatalf("Wake without a reply: ok=%v err=%v", ok, err)
	}
	if len(f.starts) != 0 {
		t.Fatalf("started %d process(es) with nothing to read", len(f.starts))
	}
}

func TestWakeLeavesRunningMergeAlone(t *testing.T) {
	f := newFixture(t)
	lock, ok, err := AcquireLock()
	if err != nil || !ok {
		t.Fatalf("lock: ok=%v err=%v", ok, err)
	}
	f.set(t, func(s *bgreg.Status) { s.MergeState = bgreg.MergeStateMerging })
	_ = bgreg.InboxAppend(testID, "hold on")
	if ok, _ := Wake(f.repo, f.status(t)); ok {
		t.Fatal("woke an agent whose merge is in flight")
	}
	// once the coordinator is gone the stale "merging" mark must not pin it
	lock.Release()
	if ok, _ := Wake(f.repo, f.status(t)); !ok {
		t.Fatal("agent stayed asleep after the merge lock was released")
	}
}

func TestConcurrentWakesStartOneProcess(t *testing.T) {
	f := newFixture(t)
	_ = bgreg.InboxAppend(testID, "go")
	st := f.status(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	woke := 0
	for range 4 {
		wg.Go(func() {
			if ok, _ := Wake(f.repo, st); ok {
				mu.Lock()
				woke++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if woke != 1 || len(f.starts) != 1 {
		t.Fatalf("want exactly one wake, got woke=%d starts=%d", woke, len(f.starts))
	}
}

// the main TUI has no merger: it finds the repo from the worktree itself
func TestWakeWaitingFindsRepoFromWorktree(t *testing.T) {
	f := newFixture(t)
	_ = bgreg.InboxAppend(testID, "ping")
	WakeWaiting([]bgreg.Status{f.status(t)})
	if len(f.starts) != 1 {
		t.Fatalf("want one start, got %d", len(f.starts))
	}
}
