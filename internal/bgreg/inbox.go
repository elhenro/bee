// Inbox IPC. Agent-view TUI appends user follow-up messages to a
// per-session JSONL file; bg loop tails it from a byte cursor at every
// turn boundary.
package bgreg

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Inbox struct {
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

func inboxPath(sessionID string) (string, error) {
	d, err := dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, sessionID+".inbox.jsonl"), nil
}

func InboxAppend(sessionID, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("inbox: empty text")
	}
	p, err := inboxPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(Inbox{Text: text, CreatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	line = append(line, '\n')
	_, err = f.Write(line)
	return err
}

// InboxDrain returns every line written after cursor and the new cursor.
// Pass 0 on first call. Cursor lives in caller memory only.
func InboxDrain(sessionID string, cursor int64) ([]Inbox, int64, error) {
	p, err := inboxPath(sessionID)
	if err != nil {
		return nil, cursor, err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, cursor, nil
		}
		return nil, cursor, err
	}
	defer f.Close()
	if _, err := f.Seek(cursor, io.SeekStart); err != nil {
		return nil, cursor, err
	}
	var out []Inbox
	r := bufio.NewReader(f)
	advanced := cursor
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var m Inbox
			if jerr := json.Unmarshal(line[:len(line)-1], &m); jerr == nil {
				out = append(out, m)
				advanced += int64(len(line))
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return out, advanced, err
		}
	}
	return out, advanced, nil
}

func InboxRemove(sessionID string) error {
	p, err := inboxPath(sessionID)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
