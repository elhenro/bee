//go:build !windows

package pidlock

import (
	"errors"
	"os"
	"syscall"
)

type lockImpl struct{ f *os.File }

// Acquire takes the lock at path without blocking. A live holder yields a
// *HeldError. info is stored after the holder's pid for diagnostics.
func Acquire(path, info string) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		held := errors.Is(err, syscall.EWOULDBLOCK)
		body, _ := os.ReadFile(path)
		_ = f.Close()
		if held {
			return nil, &HeldError{Path: path, Info: string(body)}
		}
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(record(os.Getpid(), info)), 0)
	return &Lock{impl: lockImpl{f: f}}, nil
}

// Held reports whether a live process holds the lock at path.
func Held(path string) bool {
	l, err := Acquire(path, "probe")
	if err != nil {
		var held *HeldError
		return errors.As(err, &held)
	}
	l.Release()
	return false
}

func (l lockImpl) release() {
	if l.f == nil {
		return
	}
	_ = l.f.Truncate(0)
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
}
