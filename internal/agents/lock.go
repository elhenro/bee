package agents

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MergeLock is an advisory single-host lock for the merge coordinator. Held
// for the full rebase+FF window so two agents can't race onto main. Stale
// locks (dead PID) are stolen on acquire.
type MergeLock struct {
	path string
}

// AcquireLock attempts to take the merge lock. Returns ok=false (no error)
// when another live process holds it; the caller can retry later.
func AcquireLock() (*MergeLock, bool, error) {
	p, err := MergeLockPath()
	if err != nil {
		return nil, false, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return &MergeLock{path: p}, true, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, false, err
		}
		// existing lock — check liveness
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil, false, rerr
		}
		pidStr := strings.TrimSpace(string(body))
		pid, perr := strconv.Atoi(pidStr)
		if perr != nil || pid <= 0 || !pidAlive(pid) {
			// stale — steal
			_ = os.Remove(p)
			continue
		}
		return nil, false, nil
	}
	return nil, false, errors.New("merge lock: contention after stale steal")
}

// Release removes the lock file. Safe to call on a nil receiver.
func (l *MergeLock) Release() {
	if l == nil || l.path == "" {
		return
	}
	_ = os.Remove(l.path)
}
