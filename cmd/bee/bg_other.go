//go:build !darwin && !linux

package main

import "os/exec"

// detach is a no-op on platforms without Setsid (windows). Background
// behavior on those targets is best-effort and not feature-complete.
func detach(cmd *exec.Cmd) {}
