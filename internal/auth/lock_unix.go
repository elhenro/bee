//go:build !windows

package auth

import (
	"os"
	"path/filepath"
	"syscall"
)

// LockToken takes a cross-process advisory flock guarding a provider's token
// refresh, so two bee processes never spend the same rotating refresh_token.
// Best-effort: any failure returns a no-op unlock. The kernel drops the lock if
// the holder crashes.
func LockToken(dir, provider string) func() {
	f, err := os.OpenFile(filepath.Join(dir, provider+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
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
