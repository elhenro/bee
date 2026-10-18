//go:build windows

package pidlock

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

type lockImpl struct{ path string }

// Acquire takes the lock at path without blocking. Windows has no flock here,
// so the lock is the file's existence; a file whose recorded pid is dead is
// stolen.
func Acquire(path, info string) (*Lock, error) {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(record(os.Getpid(), info))
			_ = f.Close()
			return &Lock{impl: lockImpl{path: path}}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		body, _ := os.ReadFile(path)
		if holderAlive(string(body)) {
			return nil, &HeldError{Path: path, Info: string(body)}
		}
		_ = os.Remove(path)
	}
	return nil, &HeldError{Path: path}
}

// Held reports whether a live process holds the lock at path.
func Held(path string) bool {
	body, err := os.ReadFile(path)
	return err == nil && holderAlive(string(body))
}

func (l lockImpl) release() {
	if l.path != "" {
		_ = os.Remove(l.path)
	}
}

func holderAlive(body string) bool {
	line, _, _ := strings.Cut(body, "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}
