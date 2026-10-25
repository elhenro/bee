package agents

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
)

var errNotWakeable = errors.New("agent not wakeable")

// Wake restarts an agent whose process exited so a reply waiting in its inbox
// gets read: the new process takes the inbox on startup. The claim is a state
// flip under bgreg.Update, so a merger scan and an agent view that find the
// same agent start one process between them. Unlike Resume it spends no
// budget, a reply is the user driving.
func Wake(repoRoot string, st bgreg.Status) (bool, error) {
	err := bgreg.Update(st.SessionID, func(s *bgreg.Status) error {
		if !wakeable(*s) {
			return errNotWakeable
		}
		s.State = bgreg.StateActive
		s.MergeState = bgreg.MergeStateUnmerged
		s.ConflictMsg = ""
		s.PID = 0
		s.FinishedAt = time.Time{}
		st = *s
		return nil
	})
	if errors.Is(err, errNotWakeable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return restart(repoRoot, st, wakePrompt(st.Task), "wake")
}

// WakeWaiting wakes every listed agent that is stopped with a reply waiting.
// The repo is read off each worktree, so callers need not know it.
func WakeWaiting(all []bgreg.Status) {
	for _, s := range all {
		if !wakeable(s) {
			continue
		}
		cd, err := commonDir(s.WorktreePath)
		if err != nil {
			continue
		}
		_, _ = Wake(filepath.Dir(cd), s)
	}
}

func wakePrompt(task string) string {
	return task + priorWork + "You signalled you were done or stuck, then the user sent you a follow-up. It is below."
}
