// Background-task helpers. `bee bg` spawns detached child engines and
// drops their stdout/stderr under <beeHome>/sessions/bg/<session-id>.log.
// Pure helpers — no exec, no syscall — so the package stays portable.
package hive

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/elhenro/bee/internal/bgreg"
)

// LogPath returns the absolute path where a background session's combined
// stdout/stderr should be written. It does not create anything on disk.
func LogPath(sessionID string) (string, error) { return bgreg.File(sessionID, ".log") }

// EnsureLogDir creates the parent directory for LogPath and returns the
// fully-qualified log path. Idempotent.
func EnsureLogDir(sessionID string) (string, error) {
	p, err := LogPath(sessionID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, nil
}

// LogDir returns the directory holding background log files.
func LogDir() (string, error) { return bgreg.Dir() }

// WritePid writes the process id to a pidfile beside the log.
func WritePid(sessionID string, pid int) error {
	path, err := bgreg.File(sessionID, ".pid")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

// ReadPid reads a pidfile if it exists.
func ReadPid(sessionID string) (int, error) {
	path, err := bgreg.File(sessionID, ".pid")
	if err != nil {
		return 0, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
