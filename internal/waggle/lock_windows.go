//go:build windows

package waggle

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive LockFileEx lock on path+".lock" and returns the
// unlock. Best-effort like the unix flock: on any failure it returns a no-op
// and the caller proceeds unlocked. The OS drops the lock if the holder dies.
func lockFile(path string) func() {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol); err != nil {
		_ = f.Close()
		return func() {}
	}
	return func() {
		_ = windows.UnlockFileEx(h, 0, 1, 0, ol)
		_ = f.Close()
	}
}
