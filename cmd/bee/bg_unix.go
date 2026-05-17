//go:build darwin || linux

package main

import (
	"os/exec"
	"syscall"
)

// detach puts the child in its own session so it survives the parent
// process exit and is decoupled from the controlling terminal.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
