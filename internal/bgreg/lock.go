//go:build windows

package bgreg

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// sessionLock is a per-session advisory lock used by Update to serialize
// read-modify-write cycles. Stored next to the status JSON as <id>.lock.
// Backed by Windows LockFileEx with LOCKFILE_EXCLUSIVE_LOCK so a crashed
// holder's lock is released by the OS when the handle closes.
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
		var ol windows.Overlapped
		err := windows.LockFileEx(
			windows.Handle(f.Fd()),
			windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
			0, 1, 0, &ol,
		)
		if err == nil {
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
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(l.f.Fd()), 0, 1, 0, &ol)
	_ = l.f.Close()
	_ = os.Remove(l.path)
}

func lockPath(d, id string) string { return d + string(os.PathSeparator) + id + ".lock" }
