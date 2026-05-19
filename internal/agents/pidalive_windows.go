//go:build windows

package agents

import "golang.org/x/sys/windows"

// pidAlive returns true when the OS still has a process with that pid.
// Windows has no signal-0 equivalent — open the process and check exit code.
// STILL_ACTIVE (259) means the process is running.
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// access-denied still implies the pid exists
		if err == windows.ERROR_ACCESS_DENIED {
			return true
		}
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		// handle valid → assume alive
		return true
	}
	const stillActive = 259
	return code == stillActive
}
