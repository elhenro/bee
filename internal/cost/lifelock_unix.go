//go:build !windows

package cost

import (
	"os"
	"syscall"
)

// lockLifetimeFile takes a cross-process advisory flock guarding the lifetime
// totals read-modify-write. Best-effort: any failure returns a no-op unlock
// and the caller proceeds unlocked (worst case is the old lost-update drift).
// Kernel releases the lock if the holder crashes; no stale-pid handling.
func lockLifetimeFile(path string) func() {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
}
