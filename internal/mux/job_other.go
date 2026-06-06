//go:build windows

package mux

import "os/exec"

// JobSupported reports that job-control launch is unavailable on this platform.
func JobSupported() bool { return false }

// StartJob is unsupported; callers fall back to a plain blocking run.
func StartJob(cmd *exec.Cmd, _ uintptr) (bool, int, error) { return false, 0, cmd.Run() }

// ResumeJob is a no-op on platforms without job control.
func ResumeJob(int, uintptr) (bool, error) { return false, nil }
