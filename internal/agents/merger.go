package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/zzz"
)

// MergeBase is the branch every agent rebases onto. Hardcoded for v1.
const MergeBase = "main"

// MergeAttempt is the outcome of one merge pass.
type MergeAttempt struct {
	SessionID string
	OK        bool
	Conflict  bool
	Err       error
	Output    string
}

// TryMerge attempts to rebase an agent's branch onto MergeBase and FF-merge
// the result back into MergeBase. The merge lock is held for the full
// rebase+FF window so concurrent agents serialize through this.
//
// Outcomes:
//   - ok=true: branch merged into main.
//   - conflict=true: rebase hit conflict; an inbox message was posted to the
//     agent to ask it to resolve. Status flips to awaiting.
//   - err != nil with neither flag: something unexpected; status preserved.
func TryMerge(ctx context.Context, repoRoot string, st bgreg.Status) MergeAttempt {
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
	_ = bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
		s.MergeState = bgreg.MergeStateMerging
		return nil
	})

	// fetch is best-effort: local-only repos have no origin.
	_ = zzz.Fetch(repoRoot, "origin")

	// rebase agent branch onto main inside the worktree.
	rebaseErr := zzz.Rebase(st.WorktreePath, MergeBase)
	if rebaseErr != nil {
		conflictedFiles := parseConflictFiles(rebaseErr.Error())
		_ = zzz.RebaseAbort(st.WorktreePath)

		_ = bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
			s.MergeState = bgreg.MergeStateConflict
			s.ConflictMsg = rebaseErr.Error()
			s.State = bgreg.StateAwaiting
			return nil
		})

		// auto-retrigger the agent with a conflict-resolution prompt.
		var fileList string
		if len(conflictedFiles) > 0 {
			fileList = " Conflicting files:\n- " + strings.Join(conflictedFiles, "\n- ")
		}
		msg := fmt.Sprintf(
			"Your branch %s could not rebase onto %s.%s\n\nPlease check `git status`, resolve the conflicts in your worktree (%s), commit, and end your reply with DONE: <summary>. The coordinator will retry the merge automatically.",
			st.Branch, MergeBase, fileList, st.WorktreePath,
		)
		_ = bgreg.InboxAppend(st.SessionID, msg)

		res.Conflict = true
		res.Output = rebaseErr.Error()
		return res
	}

	// FF-merge agent branch back into main from the main repo dir.
	if err := zzz.MergeFF(repoRoot, MergeBase, st.Branch); err != nil {
		// rare: usually means base advanced between rebase and merge; flip
		// back to unmerged and let the next tick retry.
		_ = bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
			s.MergeState = bgreg.MergeStateUnmerged
			s.ConflictMsg = "ff-merge failed: " + err.Error()
			return nil
		})
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

// MergerLoop wakes every interval and tries to merge any agent that's done
// but not yet merged. Caller cancels via ctx. retryCh delivers manual retry
// signals (session id) — these bypass the timer and try immediately.
func MergerLoop(ctx context.Context, repoRoot string, interval time.Duration, retryCh <-chan string) {
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
			scan(ctx, repoRoot, "")
		case id := <-retryCh:
			scan(ctx, repoRoot, id)
		}
	}
}

func scan(ctx context.Context, repoRoot, only string) {
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
		if !mergeable(s) {
			continue
		}
		TryMerge(ctx, repoRoot, s)
	}
}

// mergeable returns true when the agent is in a state we should attempt to
// merge. Done + (unmerged | conflict) qualifies; merging is already in
// flight elsewhere; merged is finished.
func mergeable(s bgreg.Status) bool {
	if s.Branch == "" || s.WorktreePath == "" {
		return false
	}
	if s.MergeState == bgreg.MergeStateMerged ||
		s.MergeState == bgreg.MergeStateMerging {
		return false
	}
	// agent must have signalled done OR be in conflict (user requested retry)
	if s.State == bgreg.StateDone {
		return true
	}
	if s.MergeState == bgreg.MergeStateConflict {
		return true
	}
	return false
}
