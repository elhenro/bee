//go:build !windows

package tools

import (
	"os/exec"
	"syscall"
	"time"
)

// GroupKill runs cmd in its own process group and makes context cancel kill the
// whole group, so backgrounded children die with the leader. waitDelay caps how
// long Wait blocks on pipes a surviving grandchild still holds open. Call
// before Start; cmd must come from exec.CommandContext.
func GroupKill(cmd *exec.Cmd, waitDelay time.Duration) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = waitDelay
}

func killGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == syscall.ESRCH || err == syscall.EPERM {
		return nil
	}
	return err
}
