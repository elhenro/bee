package bgreg

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The read position lives on disk next to the inbox, so whichever process
// owns the session picks up where the last one stopped: a restarted agent
// neither replays messages an earlier process took nor misses ones that
// arrived while no process was listening. One consumer at a time; callers
// claim the session first (see agents.Wake).

func cursorPath(sessionID string) (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, sessionID+".inbox.cursor"), nil
}

func loadCursor(sessionID string) int64 {
	p, err := cursorPath(sessionID)
	if err != nil {
		return 0
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return 0
	}
	c, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || c < 0 {
		return 0
	}
	return c
}

func saveCursor(sessionID string, c int64) error {
	p, err := cursorPath(sessionID)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), sessionID+"-cursor-*.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(strconv.FormatInt(c, 10)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// InboxPending reports whether unread messages are waiting, without taking them.
func InboxPending(sessionID string) bool {
	msgs, _, err := InboxDrain(sessionID, loadCursor(sessionID))
	return err == nil && len(msgs) > 0
}

// InboxTake returns the unread messages and marks them read. When the new
// position cannot be saved nothing is handed out, so the messages stay unread
// instead of coming back again.
func InboxTake(sessionID string) ([]Inbox, error) {
	cur := loadCursor(sessionID)
	msgs, next, err := InboxDrain(sessionID, cur)
	if err != nil {
		return nil, err
	}
	if next != cur {
		if err := saveCursor(sessionID, next); err != nil {
			return nil, err
		}
	}
	return msgs, nil
}

func cursorRemove(sessionID string) {
	if p, err := cursorPath(sessionID); err == nil {
		_ = os.Remove(p)
	}
}
