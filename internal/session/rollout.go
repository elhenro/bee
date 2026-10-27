package session

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/elhenro/bee/internal/types"
)

// maxLineBytes bounds a single JSONL line. Tool results and large pasted
// content can be sizable; 8 MiB is a safe ceiling.
const maxLineBytes = 8 * 1024 * 1024

// rolloutFile is the slice of *os.File a Rollout uses once opened.
type rolloutFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

// Rollout is an append-only JSONL writer for one session.
// Single open handle; fsync runs on a background syncer (rollout_sync.go), never
// on the Append path. Not safe for cross-process use; callers are expected to
// own the session.
type Rollout struct {
	id string

	mu      sync.Mutex
	f       rolloutFile
	torn    bool    // a failed write may have left a partial line
	dirty   bool    // written since the last fsync
	closing bool    // Close started: no new syncer
	sy      *syncer // lazily started on the first Append
}

// Open opens (or creates) the rollout file for session id.
func Open(id string) (*Rollout, error) {
	if id == "" {
		return nil, errors.New("session: empty id")
	}
	dir, err := ensureDir()
	if err != nil {
		return nil, err
	}
	p, err := Path(id)
	if err != nil {
		return nil, err
	}
	pruneEmpty(dir, p)
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	if err := terminateTornLine(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Rollout{id: id, f: f}, nil
}

// terminateTornLine ends a last line a crash left without its newline. Without
// it the next Append glues onto the fragment and both messages are lost.
func terminateTornLine(f *os.File) error {
	fi, err := f.Stat()
	if err != nil || fi.Size() == 0 {
		return err
	}
	var last [1]byte
	if _, err := f.ReadAt(last[:], fi.Size()-1); err != nil || last[0] == '\n' {
		return err
	}
	_, err = f.Write([]byte{'\n'})
	return err
}

// ID returns the session id this rollout writes.
func (r *Rollout) ID() string { return r.id }

// Append writes one message as a single JSON line. The write lands in the page
// cache immediately (readers and a killed process see it); the fsync that guards
// against power loss is handed to the background syncer, so a slow disk never
// sits on the turn hot path. Context is honored only for cancellation before the
// write begins; stdlib file I/O is uninterruptible.
func (r *Rollout) Append(ctx context.Context, msg types.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := marshalLine(msg)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return errors.New("session: rollout closed")
	}
	if r.torn {
		// end the fragment a failed write left, or this line glues onto it
		b = append([]byte{'\n'}, b...)
	}
	if n, err := r.f.Write(b); err != nil {
		r.torn = r.torn || n > 0
		return err
	}
	r.torn = false
	r.dirty = true
	r.kickLocked()
	return nil
}

// Close stops the syncer, fsyncs anything still pending, then releases the file
// handle.
func (r *Rollout) Close() error {
	r.mu.Lock()
	if r.f == nil {
		r.mu.Unlock()
		return nil
	}
	r.closing = true
	s := r.sy
	r.sy = nil
	r.mu.Unlock()
	if s != nil {
		s.shutdown()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	var syncErr error
	if r.dirty {
		syncErr = r.f.Sync()
		r.dirty = false
	}
	closeErr := r.f.Close()
	r.f = nil
	return errors.Join(syncErr, closeErr)
}
