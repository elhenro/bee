package bgreg

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// sessionLock is a per-session advisory lock used by Update to serialize
// read-modify-write cycles. Stored next to the status JSON as <id>.lock. The
// OS holds it (tryLock/unlock per platform), so a crashed holder's lock is
// released when its handle closes; no stale-pid heuristic needed.
type sessionLock struct {
	path string
	f    *os.File
}

func acquireSessionLock(id string) (*sessionLock, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	p := lockPath(d, id)
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		ok, err := tryLock(f)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if ok {
			// record pid for diagnostics. Truncate first since prior holder
			// may have written its own pid.
			_ = f.Truncate(0)
			_, _ = f.Seek(0, 0)
			fmt.Fprintf(f, "%d\n", os.Getpid())
			return &sessionLock{path: p, f: f}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, errors.New("bgreg: session lock contention")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (l *sessionLock) release() {
	if l == nil || l.f == nil {
		return
	}
	// unlock explicitly so a later reopen in the same process can't
	// accidentally hold it.
	unlock(l.f)
	_ = l.f.Close()
	// never unlink: a waiter already holding an fd on this inode would lock
	// the old file while the next arrival creates a new one, giving two
	// holders. Remove deletes the lock with the session.
}

func lockPath(d, id string) string { return filepath.Join(d, id+".lock") }
