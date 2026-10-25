package agents

import (
	"errors"

	"github.com/elhenro/bee/internal/pidlock"
)

// AcquireLock attempts to take the merge lock: an advisory single-host lock
// held for the full rebase+FF window so two agents can't race onto main. Backed
// by a kernel lock, so a crashed holder frees it with no stale-pid heuristics.
// Returns ok=false (no error) when another live process holds it; the caller
// can retry later. Release is safe on a nil lock.
func AcquireLock() (l *pidlock.Lock, ok bool, err error) {
	p, err := MergeLockPath()
	if err != nil {
		return nil, false, err
	}
	l, err = pidlock.Acquire(p, "merge")
	var held *pidlock.HeldError
	if errors.As(err, &held) {
		return nil, false, nil
	}
	return l, err == nil, err
}

// mergeLockHeld reports whether a live process currently holds the merge
// lock. Used to tell an in-flight merge apart from a stale "merging" state
// left behind by a coordinator that crashed mid-merge.
func mergeLockHeld() bool {
	p, err := MergeLockPath()
	return err == nil && pidlock.Held(p)
}
