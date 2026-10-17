//go:build !windows

package gate

import (
	"errors"
	"os"
	"syscall"
)

func tryFlock(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return false, err
}

func unflock(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }
