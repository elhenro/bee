package agents

import (
	"os"

	"github.com/elhenro/bee/internal/bgreg"
)

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

// wakeable is true for an agent whose process has exited (DONE, BLOCKED or an
// error) while a reply sits unread in its inbox. A blocked agent counts: the
// reply is the human it was waiting for. A merge in flight is left alone, the
// agent is woken once it ends.
func wakeable(s bgreg.Status) bool {
	if s.Branch == "" || s.WorktreePath == "" {
		return false
	}
	if s.State != bgreg.StateDone && s.State != bgreg.StateFailed {
		return false
	}
	if s.MergeState == bgreg.MergeStateMerging && mergeLockHeld() {
		return false
	}
	if _, err := os.Stat(s.WorktreePath); err != nil {
		return false
	}
	return bgreg.InboxPending(s.SessionID)
}
