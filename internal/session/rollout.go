package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/elhenro/bee/internal/types"
)

// maxLineBytes bounds a single JSONL line. Tool results and large pasted
// content can be sizable; 8 MiB is a safe ceiling.
const maxLineBytes = 8 * 1024 * 1024

// Rollout is an append-only JSONL writer for one session.
// Single open handle, sync-on-append. Not safe for cross-process use; callers
// are expected to own the session.
type Rollout struct {
	id   string
	path string

	mu sync.Mutex
	f  *os.File
}

// Open opens (or creates) the rollout file for session id.
func Open(id string) (*Rollout, error) {
	if id == "" {
		return nil, errors.New("session: empty id")
	}
	if _, err := ensureDir(); err != nil {
		return nil, err
	}
	p, err := Path(id)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Rollout{id: id, path: p, f: f}, nil
}

// ID returns the session id this rollout writes.
func (r *Rollout) ID() string { return r.id }

// Path returns the on-disk file path.
func (r *Rollout) FilePath() string { return r.path }

// Append writes one message as a single JSON line, then fsyncs.
// Context is honored only for cancellation before the write begins;
// stdlib file I/O is uninterruptible mid-call.
func (r *Rollout) Append(ctx context.Context, msg types.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return errors.New("session: rollout closed")
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := r.f.Write(b); err != nil {
		return err
	}
	return r.f.Sync()
}

// Close releases the file handle.
func (r *Rollout) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// Read decodes all messages in a session file, in append order.
func Read(id string) ([]types.Message, error) {
	p, err := Path(id)
	if err != nil {
		return nil, err
	}
	return readFile(p)
}

func readFile(path string) ([]types.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)

	var out []types.Message
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m types.Message
		if err := json.Unmarshal(line, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return out, nil
}

// readFirstMessage reads only the first JSON line. Cheap for List().
func readFirstMessage(path string) (types.Message, error) {
	var m types.Message
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return m, err
		}
		return m, io.EOF
	}
	if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
		return m, err
	}
	return m, nil
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
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		p := filepath.Join(dir, name)
		first, err := readFirstMessage(p)
		if err != nil {
			// empty or malformed file: skip but don't fail the whole listing
			if errors.Is(err, io.EOF) {
				continue
			}
			return nil, err
		}
		s := types.Session{ID: id, Created: first.Time}
		if s.Created.IsZero() {
			if fi, err := e.Info(); err == nil {
				s.Created = fi.ModTime()
			} else {
				s.Created = time.Time{}
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// FirstUserText returns the first user-role text block in a session, scanning
// at most maxScanLines lines. Empty if the session has no user text yet.
func FirstUserText(id string) (string, error) {
	p, err := Path(id)
	if err != nil {
		return "", err
	}
	return firstUserTextFromFile(p)
}

const maxScanLines = 32

func firstUserTextFromFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), maxLineBytes)
	scanned := 0
	for sc.Scan() {
		scanned++
		if scanned > maxScanLines {
			break
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var m types.Message
		if err := json.Unmarshal(line, &m); err != nil {
			return "", err
		}
		if m.Role != types.RoleUser {
			continue
		}
		for _, b := range m.Content {
			if b.Type == types.BlockText {
				if s := strings.TrimSpace(b.Text); s != "" {
					return s, nil
				}
			}
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return "", nil
}
