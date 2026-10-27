package agents

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/gate"
	"github.com/elhenro/bee/internal/zzz"
)

// MergeBase is the branch an agent merges into when its status records none
// (spawned detached, or by a build that predates Status.Base).
const MergeBase = "main"

// baseOf is the branch st rebases onto and fast-forwards.
func baseOf(st bgreg.Status) string {
	if st.Base != "" {
		return st.Base
	}
	return MergeBase
}

// MergeAttempt is the outcome of one merge pass.
type MergeAttempt struct {
	SessionID string
	OK        bool
	Conflict  bool
	Red       bool // verify failed on the rebased tree
	Err       error
	Output    string
}

// TryMerge rebases an agent's branch onto its base branch (the one it was
// forked from), runs the repo's verify gate on the rebased tree, and
// fast-forwards that base. The merge lock is
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
	base := baseOf(st)
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

	// a rebase the agent left half-done is its own work: hand it back as is
	if rebasing(st.WorktreePath) {
		setMerge(st.SessionID, bgreg.MergeStateConflict, "rebase left in progress")
		_, res.Err = Resume(repoRoot, st, conflictFeedback(st, unmergedFiles(st.WorktreePath)), sw.Resumes())
		res.Conflict = true
		return res
	}
	if rebaseErr := zzz.Rebase(st.WorktreePath, base); rebaseErr != nil {
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
	if gateLinked(repoRoot) {
		Block(st.SessionID, gate.File+" is a symlink in the main checkout, make it a regular file")
		return res
	}
	if touchesGate(st.WorktreePath, base) {
		Block(st.SessionID, "branch edits "+filepath.Dir(gate.File)+", a human must review the gate change")
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
	v, err := verifyPinned(ctx, repoRoot, pinned, sw)
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
	if err := zzz.MergeFF(repoRoot, base, pinned); err != nil {
		// usually base advanced between rebase and merge, so retry next tick.
		// but a base checked out elsewhere or a dirty main fails every tick and
		// re-verifies each time: spend a hop per failure, then park for a human.
		msg := "ff-merge failed: " + err.Error()
		hops := st.Hops + 1
		_ = bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
			s.Hops, s.MergeState, s.ConflictMsg = hops, bgreg.MergeStateUnmerged, msg
			return nil
		})
		if hops > sw.Hops() {
			Block(st.SessionID, fmt.Sprintf("%s (failed %d times)", msg, hops))
		}
		res.Err = err
		return res
	}

	// main already moved: surface a failed bookkeeping write instead of
	// dropping it, the next tick would re-verify an already merged branch.
	res.Err = bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
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
		st.Branch, baseOf(st), hops, sw.Hops(), summary, st.WorktreePath)
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
	base := baseOf(st)
	return fmt.Sprintf(
		"Your branch %s could not rebase onto %s.%s\n\nRebase it onto %s in your worktree (%s), resolve the conflicts, commit, and end your reply with DONE: <summary>. The coordinator retries the merge automatically.",
		st.Branch, base, fileList, base, st.WorktreePath,
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
	sweepVerify(repoRoot)
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
		if s.WorktreePath == "" || !sameRepo(repoRoot, s.WorktreePath) {
			continue
		}
		switch {
		case wakeable(s):
			_, _ = Wake(repoRoot, s)
		case mergeable(s):
			TryMerge(ctx, repoRoot, s, sw)
		case crashed(s):
			_, _ = Resume(repoRoot, s, "Your process stopped before it finished. Continue the task from where the branch is now.", sw.Resumes())
		}
	}
}

// verifyPinned runs the gate on a fresh checkout of exactly the pinned commit.
func verifyPinned(ctx context.Context, repoRoot, pinned string, sw config.SwarmConfig) (gate.Result, error) {
	cmds := gate.Commands(repoRoot, sw.Verify)
	if len(cmds) == 0 {
		return gate.Result{OK: true}, nil
	}
	dir, cleanup, err := pristine(repoRoot, pinned)
	if err != nil {
		return gate.Result{}, err
	}
	defer cleanup()
	res, err := gate.Run(ctx, dir, cmds, gate.Opts{Timeout: sw.VerifyTimeout(), Scope: agentScope})
	// a checkout that moved off pinned verified something else
	if head, herr := gitOut(dir, "rev-parse", "HEAD"); err == nil && (herr != nil || head != pinned) {
		return gate.Result{Ran: res.Ran, Failed: "checkout integrity", Output: "verify checkout moved off " + pinned}, nil
	}
	return res, err
}
