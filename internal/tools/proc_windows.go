//go:build windows

package tools

import (
	"os/exec"
	"time"
)

// GroupKill only sets the pipe wait cap on windows; there is no process group
// to signal, so cancel falls back to killing the leader.
func GroupKill(cmd *exec.Cmd, waitDelay time.Duration) {
	cmd.WaitDelay = waitDelay
}
