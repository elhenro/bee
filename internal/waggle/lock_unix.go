//go:build !windows

package waggle

import (
	"os"
	"syscall"
)

// lockFile takes a cross-process advisory flock on path+".lock" and returns
// the unlock. Best-effort: on any failure it returns a no-op and the caller
// proceeds unlocked. The kernel drops the lock if the holder crashes.
func lockFile(path string) func() {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
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
