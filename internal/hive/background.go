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
)

// env override matches internal/skills/paths.go so users get one knob.
const envBeeHome = "BEE_HOME"

// LogPath returns the absolute path where a background session's combined
// stdout/stderr should be written. It does not create anything on disk.
func LogPath(sessionID string) (string, error) {
	dir, err := bgLogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionID+".log"), nil
}

// EnsureLogDir creates the parent directory for LogPath and returns the
// fully-qualified log path. Idempotent.
func EnsureLogDir(sessionID string) (string, error) {
	dir, err := bgLogDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, sessionID+".log"), nil
}

// LogDir returns the directory holding background log files.
func LogDir() (string, error) {
	return bgLogDir()
}

// bgLogDir resolves <beeHome>/sessions/bg without touching disk.
func bgLogDir() (string, error) {
	home, err := beeHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "sessions", "bg"), nil
}

// beeHome mirrors internal/skills.beeHome: BEE_HOME env wins, else ~/.bee.
func beeHome() (string, error) {
	if v := os.Getenv(envBeeHome); v != "" {
		return v, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".bee"), nil
}

// WritePid writes the process id to a pidfile beside the log.
func WritePid(sessionID string, pid int) error {
	dir, err := bgLogDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, sessionID+".pid")
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

// ReadPid reads a pidfile if it exists.
func ReadPid(sessionID string) (int, error) {
	dir, err := bgLogDir()
	if err != nil {
		return 0, err
	}
	path := filepath.Join(dir, sessionID+".pid")
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
