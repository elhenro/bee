//go:build !windows

package gate

import (
	"os/exec"
	"syscall"
)

// setGroupKill makes the command a group leader so cancel reaches every
// child: a hung `go test` binary must not outlive its gate and hold the lock.
func setGroupKill(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
