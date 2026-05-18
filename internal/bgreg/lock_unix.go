//go:build !windows

package bgreg

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// sessionLock is a per-session advisory lock used by Update to serialize
// read-modify-write cycles. Stored next to the status JSON as <id>.lock.
// Backed by syscall.Flock with LOCK_EX | LOCK_NB so a crashed holder's lock
// is auto-released by the kernel; no stale-pid heuristic needed.
type sessionLock struct {
	path string
	f    *os.File
}

func acquireSessionLock(id string) (*sessionLock, error) {
	d, err := dir()
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
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			// record pid for diagnostics. Truncate first since prior holder
			// may have written its own pid.
			_ = f.Truncate(0)
			_, _ = f.Seek(0, 0)
			fmt.Fprintf(f, "%d\n", os.Getpid())
			return &sessionLock{path: p, f: f}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
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
	// flock is released by the kernel on close, but unlock explicitly so a
	// later reopen in the same process can't accidentally hold it.
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	// best-effort cleanup; another waiter may already hold a new lock on
	// this path so Remove failing is fine.
	_ = os.Remove(l.path)
}

func lockPath(d, id string) string { return d + string(os.PathSeparator) + id + ".lock" }
