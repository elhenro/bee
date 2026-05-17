//go:build !darwin && !linux

package agents

import "os/exec"

// detach is a no-op on platforms without Setsid (windows).
func detach(cmd *exec.Cmd) {}
