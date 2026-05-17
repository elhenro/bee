//go:build darwin || linux

package agents

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own session so it survives the parent
// process exit. Same shim as cmd/bee/bg_unix.go — duplicated here so the
// agents package can be imported without pulling in main.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
