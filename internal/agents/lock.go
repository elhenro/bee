package agents

import (
	"errors"

	"github.com/elhenro/bee/internal/pidlock"
)

// MergeLock is an advisory single-host lock for the merge coordinator. Held
// for the full rebase+FF window so two agents can't race onto main. Backed by
// a kernel lock, so a crashed holder frees it with no stale-pid heuristics.
type MergeLock struct {
	l *pidlock.Lock
}

// AcquireLock attempts to take the merge lock. Returns ok=false (no error)
// when another live process holds it; the caller can retry later.
func AcquireLock() (*MergeLock, bool, error) {
	p, err := MergeLockPath()
	if err != nil {
		return nil, false, err
	}
	l, err := pidlock.Acquire(p, "merge")
	var held *pidlock.HeldError
	if errors.As(err, &held) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &MergeLock{l: l}, true, nil
}

// Release frees the lock. Safe to call on a nil receiver.
func (l *MergeLock) Release() {
	if l == nil {
		return
	}
	l.l.Release()
}

// mergeLockHeld reports whether a live process currently holds the merge
// lock. Used to tell an in-flight merge apart from a stale "merging" state
// left behind by a coordinator that crashed mid-merge.
func mergeLockHeld() bool {
	p, err := MergeLockPath()
	return err == nil && pidlock.Held(p)
}
