//go:build !windows

package mux

import (
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// JobSupported reports whether job-control launch is available (unix).
func JobSupported() bool { return true }

// StartJob launches cmd in its own process group as the foreground job of the
// controlling terminal, then waits until it exits or is stopped (Ctrl-Z).
// Returns stopped=true with the child pid when it was suspended (still alive),
// stopped=false when it exited.
func StartJob(cmd *exec.Cmd, ttyFd uintptr) (stopped bool, pid int, err error) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Setpgid + Foreground makes the kernel place the child's new process
	// group in the terminal foreground at exec time — atomic, no race where
	// the child writes as a background job and eats a SIGTTOU.
	cmd.SysProcAttr.Setpgid = true
	cmd.SysProcAttr.Foreground = true
	cmd.SysProcAttr.Ctty = int(ttyFd)
	if err = cmd.Start(); err != nil {
		return false, 0, err
	}
	pid = cmd.Process.Pid
	stopped, err = supervise(pid, ttyFd, false)
	return stopped, pid, err
}

// ResumeJob continues a previously stopped child: hand it the terminal, send
// SIGCONT, and wait again until it stops or exits.
func ResumeJob(pid int, ttyFd uintptr) (stopped bool, err error) {
	return supervise(pid, ttyFd, true)
}

// supervise gives the terminal to the child group, optionally continues it,
// waits for stop/exit, then reclaims the terminal for bee.
func supervise(pid int, ttyFd uintptr, cont bool) (bool, error) {
	// our tcsetpgrp calls run while bee may be a background group; ignore the
	// stop signals the kernel would otherwise raise on us.
	prev := ignoreTTYStops()
	defer prev()
	defer reclaim(ttyFd)

	if cont {
		_ = tcsetpgrp(ttyFd, pid)
		if err := syscall.Kill(-pid, syscall.SIGCONT); err != nil {
			return false, err
		}
	}
	var ws unix.WaitStatus
	for {
		_, err := unix.Wait4(pid, &ws, unix.WUNTRACED, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		break
	}
	return ws.Stopped(), nil
}

// reclaim puts bee's own process group back in the terminal foreground.
func reclaim(ttyFd uintptr) { _ = tcsetpgrp(ttyFd, syscall.Getpgrp()) }

func tcsetpgrp(ttyFd uintptr, pgid int) error {
	return unix.IoctlSetPointerInt(int(ttyFd), unix.TIOCSPGRP, pgid)
}

// ignoreTTYStops silences SIGTTOU/SIGTTIN for the duration of a handoff so a
// background bee changing the terminal foreground is not itself stopped.
func ignoreTTYStops() func() {
	signal.Ignore(syscall.SIGTTOU, syscall.SIGTTIN)
	return func() { signal.Reset(syscall.SIGTTOU, syscall.SIGTTIN) }
}
