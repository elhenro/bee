package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/gate"
	"github.com/elhenro/bee/internal/zzz"
)

// MergeBase is the branch every agent rebases onto. Hardcoded for v1.
const MergeBase = "main"

// MergeAttempt is the outcome of one merge pass.
type MergeAttempt struct {
	SessionID string
	OK        bool
	Conflict  bool
	Red       bool // verify failed on the rebased tree
	Err       error
	Output    string
}

// TryMerge rebases an agent's branch onto MergeBase, runs the repo's verify
// gate on the rebased tree, and fast-forwards MergeBase. The merge lock is
// held for the whole window so agents serialize, like a single merge queue.
//
// Bounces go back to the agent through Resume:
//   - conflict: mechanical, free of hops.
//   - red verify: a real defect, costs a hop; past sw.Hops() the agent is
//     blocked for a human.
//   - HEAD moved during verify: nobody verified that commit, so the merge is
//     skipped and the next tick verifies again. Free.
func TryMerge(ctx context.Context, repoRoot string, st bgreg.Status, sw config.SwarmConfig) MergeAttempt {
	res := MergeAttempt{SessionID: st.SessionID}
	if st.Branch == "" || st.WorktreePath == "" {
		res.Err = errors.New("missing branch or worktree")
		return res
	}

	lock, ok, err := AcquireLock()
	if err != nil {
		res.Err = fmt.Errorf("acquire lock: %w", err)
		return res
	}
	if !ok {
		// another merge in flight — caller retries later
		return res
	}
	defer lock.Release()

	// flip state to merging via CAS-style Update so a concurrent agent write
	// can't clobber the transition. Same pattern for every subsequent write
	// in this function.
	setMerge(st.SessionID, bgreg.MergeStateMerging, "")

	// fetch is best-effort: local-only repos have no origin.
	_ = zzz.Fetch(repoRoot, "origin")

	if rebaseErr := zzz.Rebase(st.WorktreePath, MergeBase); rebaseErr != nil {
		// git prints CONFLICT lines on stdout, which the error does not carry,
		// so ask for the unmerged paths before the abort clears them.
		files := unmergedFiles(st.WorktreePath)
		if len(files) == 0 {
			files = parseConflictFiles(rebaseErr.Error())
		}
		_ = zzz.RebaseAbort(st.WorktreePath)
		res.Conflict = true
		res.Output = rebaseErr.Error()
		setMerge(st.SessionID, bgreg.MergeStateConflict, rebaseErr.Error())
		_, res.Err = Resume(repoRoot, st, conflictFeedback(st, files), sw.Resumes())
		return res
	}

	// untracked files would sit in the tree verify sees but never merge
	if clean, cerr := zzz.IsClean(st.WorktreePath); cerr != nil || !clean {
		setMerge(st.SessionID, bgreg.MergeStateUnmerged, "worktree not clean")
		_, res.Err = Resume(repoRoot, st, dirtyFeedback(st), sw.Resumes())
		return res
	}
	if touchesGate(st.WorktreePath) {
		Block(st.SessionID, "branch edits "+gate.File+", a human must review the gate change")
		return res
	}
	pinned, err := gitOut(st.WorktreePath, "rev-parse", "HEAD")
	if err != nil {
		setMerge(st.SessionID, bgreg.MergeStateUnmerged, err.Error())
		res.Err = err
		return res
	}
	// read the gate from the main checkout: the agent's branch must not be
	// able to weaken its own gate.
	v, err := gate.Run(ctx, st.WorktreePath, gate.Commands(repoRoot, sw.Verify), gate.Opts{Timeout: sw.VerifyTimeout(), Scope: agentScope})
	if err != nil {
		setMerge(st.SessionID, bgreg.MergeStateUnmerged, "verify: "+err.Error())
		res.Err = err
		return res
	}
	if !v.OK {
		res.Red = true
		res.Output = v.Summary()
		res.Err = bounceRed(repoRoot, st, v.Summary(), sw)
		return res
	}
	if head, _ := gitOut(st.WorktreePath, "rev-parse", "HEAD"); head != pinned || !branchAt(repoRoot, st.Branch, pinned) {
		setMerge(st.SessionID, bgreg.MergeStateUnmerged, "commits landed while verify ran, verifying again")
		return res
	}

	// FF-merge the verified commit itself, not whatever the branch names now.
	if err := zzz.MergeFF(repoRoot, MergeBase, pinned); err != nil {
		// rare: usually means base advanced between rebase and merge; flip
		// back to unmerged and let the next tick retry.
		setMerge(st.SessionID, bgreg.MergeStateUnmerged, "ff-merge failed: "+err.Error())
		res.Err = err
		return res
	}

	_ = bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
		s.MergeState = bgreg.MergeStateMerged
		s.State = bgreg.StateDone
		s.FinishedAt = time.Now().UTC()
		s.ConflictMsg = ""
		return nil
	})
	res.OK = true
	return res
}

// bounceRed spends a hop on a red verify and sends the output back, or blocks
// the agent once the hop budget is gone.
func bounceRed(repoRoot string, st bgreg.Status, summary string, sw config.SwarmConfig) error {
	hops := st.Hops + 1
	_ = bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
		s.Hops = hops
		return nil
	})
	if hops > sw.Hops() {
		Block(st.SessionID, fmt.Sprintf("verify red %d times, last: %s", hops, firstLine(summary)))
		return nil
	}
	st.Hops = hops
	msg := fmt.Sprintf("Your branch %s was rebased onto %s and the verify gate failed (hop %d/%d):\n\n%s\n\nFix it in your worktree (%s), commit, and end your reply with DONE: <summary>.",
		st.Branch, MergeBase, hops, sw.Hops(), summary, st.WorktreePath)
	_, err := Resume(repoRoot, st, msg, sw.Resumes())
	return err
}

func dirtyFeedback(st bgreg.Status) string {
	return fmt.Sprintf("Your worktree (%s) has uncommitted or untracked files, so it cannot be verified and merged. Commit what belongs to the task, delete the rest, and end your reply with DONE: <summary>.", st.WorktreePath)
}

func conflictFeedback(st bgreg.Status, files []string) string {
	var fileList string
	if len(files) > 0 {
		fileList = " Conflicting files:\n- " + strings.Join(files, "\n- ")
	}
	return fmt.Sprintf(
		"Your branch %s could not rebase onto %s.%s\n\nRebase it onto %s in your worktree (%s), resolve the conflicts, commit, and end your reply with DONE: <summary>. The coordinator retries the merge automatically.",
		st.Branch, MergeBase, fileList, MergeBase, st.WorktreePath,
	)
}

func setMerge(id string, ms bgreg.MergeState, msg string) {
	_ = bgreg.Update(id, func(s *bgreg.Status) error {
		s.MergeState = ms
		s.ConflictMsg = msg
		return nil
	})
}

// parseConflictFiles plucks file paths out of a `git rebase` error. Looks
// for "CONFLICT (content): Merge conflict in <path>" lines.
func parseConflictFiles(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "CONFLICT") {
			continue
		}
		idx := strings.Index(line, "Merge conflict in ")
		if idx < 0 {
			continue
		}
		out = append(out, strings.TrimSpace(line[idx+len("Merge conflict in "):]))
	}
	return out
}

// MergerLoop wakes every interval, merges agents that signalled DONE and
// resumes agents whose process died mid-task. Caller cancels via ctx.
func MergerLoop(ctx context.Context, repoRoot string, interval time.Duration, sw config.SwarmConfig) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			scan(ctx, repoRoot, "", sw)
		}
	}
}

func scan(ctx context.Context, repoRoot, only string, sw config.SwarmConfig) {
	if ctx.Err() != nil {
		return
	}
	all, err := bgreg.List()
	if err != nil {
		return
	}
	for _, s := range all {
		if only != "" && s.SessionID != only {
			continue
		}
		switch {
		case mergeable(s):
			TryMerge(ctx, repoRoot, s, sw)
		case crashed(s):
			_, _ = Resume(repoRoot, s, "Your process stopped before it finished. Continue the task from where the branch is now.", sw.Resumes())
		}
	}
}

// mergeable is true for an agent that signalled DONE on an unmerged branch.
// A conflict or red verify hands the work back through Resume, so an agent
// in any other state is either working or waiting for a human.
func mergeable(s bgreg.Status) bool {
	if s.Branch == "" || s.WorktreePath == "" || s.State != bgreg.StateDone {
		return false
	}
	switch s.MergeState {
	case bgreg.MergeStateMerged, bgreg.MergeStateBlocked:
		return false
	case bgreg.MergeStateMerging:
		// "merging" is in flight only while a live coordinator holds the merge
		// lock. A crashed coordinator leaves the state behind forever otherwise;
		// retrying is safe because rebase+ff-merge are idempotent.
		return !mergeLockHeld()
	}
	return true
}

// crashed is an agent-mode session marked active whose process is gone. PID 0
// means the spawner has not recorded the child yet.
func crashed(s bgreg.Status) bool {
	return s.WorktreePath != "" && s.State == bgreg.StateActive && s.PID > 0 && !pidAlive(s.PID)
}
