package session

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/elhenro/bee/internal/types"
)

// eachMessage decodes a session file line by line and calls fn with each
// message and its 1-based line number until fn returns false. Blank and corrupt
// lines (crash artifacts) are skipped instead of failing the whole read: lines
// before and after still load.
func eachMessage(path string, fn func(line int, m types.Message) bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := newLineScanner(f)
	for n := 1; sc.Scan(); n++ {
		var m types.Message
		if len(sc.Bytes()) == 0 || json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if !fn(n, m) {
			return nil
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// Read decodes all messages in a session file, in append order.
func Read(id string) ([]types.Message, error) {
	p, err := Path(id)
	if err != nil {
		return nil, err
	}
	return ReadFile(p)
}

// ReadFile decodes all messages in the rollout file at path, in append order.
func ReadFile(path string) ([]types.Message, error) {
	var out []types.Message
	err := eachMessage(path, func(_ int, m types.Message) bool {
		out = append(out, m)
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReadResume decodes a session like Read but collapses it at the last
// compaction checkpoint: the checkpoint's summary stands in for everything
// before PreserveFrom, so a resumed session sees the same shortened history the
// live session had instead of the full raw log. Falls back to the raw read when
// no checkpoint exists or PreserveFrom can't be located.
func ReadResume(id string) ([]types.Message, error) {
	msgs, err := Read(id)
	if err != nil || len(msgs) == 0 {
		return msgs, err
	}
	return collapseAtCheckpoint(msgs), nil
}

func collapseAtCheckpoint(msgs []types.Message) []types.Message {
	last := -1
	for i := range msgs {
		if msgs[i].Checkpoint != nil {
			last = i
		}
	}
	if last < 0 {
		return msgs
	}
	cp := msgs[last]
	from := slices.IndexFunc(msgs, func(m types.Message) bool { return m.ID == cp.Checkpoint.PreserveFrom })
	if from < 0 {
		// boundary lost; replay full rather than silently drop context.
		return msgs
	}
	// summary first (marker stripped so it never re-folds), then the preserved
	// tail plus everything appended after the checkpoint, minus any markers.
	cp.Checkpoint = nil
	out := make([]types.Message, 1, len(msgs)-from+1)
	out[0] = cp
	for _, m := range msgs[from:] {
		if m.Checkpoint == nil {
			out = append(out, m)
		}
	}
	return out
}

// readFirstMessage reads the first parseable JSON line. Cheap for List().
// corrupt leading lines (crash artifacts) are skipped so one bad write does
// not make the session invisible; io.EOF means no parseable line at all.
func readFirstMessage(path string) (first types.Message, err error) {
	found := false
	err = eachMessage(path, func(_ int, m types.Message) bool {
		first, found = m, true
		return false
	})
	if err == nil && !found {
		err = io.EOF
	}
	return first, err
}

// List enumerates known sessions by scanning the sessions dir. Each session's
// metadata is derived from its first message (timestamp = session created,
// id from filename). Provider/model/cwd aren't in Message; downstream code
// can enrich Session as it pleases — we fill what we can.
func List() ([]types.Session, error) {
	dir, err := SessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []types.Session
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if e.IsDir() || !ok {
			continue
		}
		first, err := readFirstMessage(filepath.Join(dir, e.Name()))
		if err != nil {
			// empty, corrupt, or unreadable file: skip it but never fail the
			// whole listing over one bad session file.
			continue
		}
		s := types.Session{ID: id, Created: first.Time}
		if s.Created.IsZero() {
			if fi, err := e.Info(); err == nil {
				s.Created = fi.ModTime()
			}
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b types.Session) int { return b.Created.Compare(a.Created) })
	return out, nil
}

// maxScanLines bounds how deep FirstUserText reads.
const maxScanLines = 32

// FirstUserText returns the first user-role text block in a session, scanning
// at most maxScanLines lines. Empty if the session has no user text yet.
func FirstUserText(id string) (string, error) {
	p, err := Path(id)
	if err != nil {
		return "", err
	}
	var text string
	err = eachMessage(p, func(line int, m types.Message) bool {
		if line > maxScanLines {
			return false
		}
		if m.Role != types.RoleUser {
			return true
		}
		for _, b := range m.Content {
			if s := strings.TrimSpace(b.Text); b.Type == types.BlockText && s != "" {
				text = s
				return false
			}
		}
		return true
	})
	return text, err
}
