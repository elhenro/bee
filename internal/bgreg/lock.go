package bgreg

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// sessionLock is a per-session advisory lock used by Update to serialize
// read-modify-write cycles. Stored next to the status JSON as <id>.lock.
type sessionLock struct{ path string }

func acquireSessionLock(id string) (*sessionLock, error) {
	d, err := dir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return nil, err
	}
	p := lockPath(d, id)
	// short bounded retry loop. Lock holders should release quickly; if a
	// process died holding the lock we steal once based on a stored pid.
	deadline := time.Now().Add(2 * time.Second)
	for {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return &sessionLock{path: p}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		// stale-holder check
		body, rerr := os.ReadFile(p)
		if rerr == nil {
			pidStr := strings.TrimSpace(string(body))
			pid, perr := strconv.Atoi(pidStr)
			if perr == nil && pid > 0 && !sessionPidAlive(pid) {
				_ = os.Remove(p)
				continue
			}
		}
		if time.Now().After(deadline) {
			return nil, errors.New("bgreg: session lock contention")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (l *sessionLock) release() {
	if l == nil || l.path == "" {
		return
	}
	_ = os.Remove(l.path)
}

func lockPath(d, id string) string { return d + string(os.PathSeparator) + id + ".lock" }

func sessionPidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		return true
	}
	return false
}
