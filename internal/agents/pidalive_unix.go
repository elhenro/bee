//go:build !windows

package agents

import (
	"errors"
	"os"
	"syscall"
)

// pidAlive returns true when the OS still has a process with that pid.
// Sending signal 0 returns nil for live processes, ESRCH for dead ones.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	// permission denied means process exists but we can't signal it
	return errors.Is(err, syscall.EPERM)
}
