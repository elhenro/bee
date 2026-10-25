//go:build windows

package bgreg

import (
	"os"

	"golang.org/x/sys/windows"
)

// tryLock takes an exclusive LockFileEx lock without blocking; any failure
// reads as "held" and the caller retries until its deadline.
func tryLock(f *os.File) (bool, error) {
	var ol windows.Overlapped
	err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &ol,
	)
	return err == nil, nil
}

func unlock(f *os.File) {
	var ol windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
