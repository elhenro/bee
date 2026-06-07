package checkpoint

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Restore rewinds the work-tree to msgID's snapshot. The current state is
// captured first (refs/bee/undo) so the rewind itself is reversible. Tracked
// files revert; files created since the snapshot are removed; gitignored paths
// are left untouched (clean has no -x).
func (s *Store) Restore(msgID string) (RestoreResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, err := s.git("rev-parse", snapRef(msgID))
	if err != nil {
		return RestoreResult{}, fmt.Errorf("no snapshot for %s", msgID)
	}
	undo, err := s.snapshotUndo()
	if err != nil {
		return RestoreResult{}, err
	}
	if _, err := s.git("reset", "--hard", target); err != nil {
		return RestoreResult{}, err
	}
	if _, err := s.git("clean", "-fd"); err != nil {
		return RestoreResult{}, err
	}
	short, _ := s.git("rev-parse", "--short", target)
	stat, _ := s.diffStat(undo, short)
	return RestoreResult{TargetSHA: short, UndoSHA: undo, ShortStat: stat}, nil
}

// DiffStat returns a one-line --shortstat between two snapshot shas. An empty
// from compares against the empty tree (useful for the first snapshot).
func (s *Store) DiffStat(from, to string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diffStat(from, to)
}

func (s *Store) diffStat(from, to string) (string, error) {
	if from == "" {
		from = emptyTree
	}
	out, err := s.git("diff", "--shortstat", from, to)
	return strings.TrimSpace(out), err
}

// Prune keeps the newest keep snapshots, drops undo refs older than 24h, and
// gcs unreferenced objects. Best-effort; failures are returned but partial.
func (s *Store) Prune(keep int) error {
	snaps, err := s.List()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if keep > 0 && len(snaps) > keep {
		for _, sn := range snaps[keep:] {
			_, _ = s.git("update-ref", "-d", snapRef(sn.MsgID))
		}
	}
	cutoff := time.Now().Add(-24 * time.Hour).UnixNano()
	out, _ := s.git("for-each-ref", "--format=%(refname)", "refs/bee/undo/")
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimPrefix(ln, "refs/bee/undo/"), 10, 64); err == nil && n < cutoff {
			_, _ = s.git("update-ref", "-d", ln)
		}
	}
	// --auto repacks only past git's own thresholds, so this stays cheap when
	// called once per turn.
	_, _ = s.git("gc", "--auto", "--quiet")
	return nil
}
