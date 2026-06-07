package checkpoint

import (
	"strconv"
	"strings"
	"time"
)

// snapRef is the ref a message's code snapshot lives under.
func snapRef(msgID string) string { return "refs/bee/snap/" + msgID }

// Snapshot records the current work-tree as the code state for msgID and
// returns the short sha. An unchanged tree reuses the existing commit.
func (s *Store) Snapshot(msgID, label string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if label == "" {
		label = "snapshot"
	}
	full, short, err := s.commitState(label)
	if err != nil {
		return "", err
	}
	if err := s.setRef(snapRef(msgID), full); err != nil {
		return "", err
	}
	return short, nil
}

// SnapshotFor returns the short sha recorded for msgID, if any.
func (s *Store) SnapshotFor(msgID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sha, err := s.git("rev-parse", "--short", snapRef(msgID))
	if err != nil {
		return "", false
	}
	return sha, true
}

// snapshotUndo commits the current work-tree under refs/bee/undo so a restore
// can be undone. Caller holds the lock.
func (s *Store) snapshotUndo() (string, error) {
	full, short, err := s.commitState("undo-snapshot")
	if err != nil {
		return "", err
	}
	ref := "refs/bee/undo/" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := s.setRef(ref, full); err != nil {
		return "", err
	}
	return short, nil
}

// List returns recorded snapshots newest first.
func (s *Store) List() ([]Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out, err := s.git("for-each-ref", "--sort=-creatordate",
		"--format=%(refname) %(objectname:short) %(creatordate:unix) %(contents:subject)",
		"refs/bee/snap/")
	if err != nil {
		return nil, err
	}
	var snaps []Snapshot
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		parts := strings.SplitN(ln, " ", 4)
		if len(parts) < 3 {
			continue
		}
		sn := Snapshot{
			MsgID: strings.TrimPrefix(parts[0], "refs/bee/snap/"),
			SHA:   parts[1],
		}
		if secs, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
			sn.Time = time.Unix(secs, 0)
		}
		if len(parts) == 4 {
			sn.Label = parts[3]
		}
		snaps = append(snaps, sn)
	}
	return snaps, nil
}
