package zzz

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// lockDir returns ~/.bee/zzz/locks, creating it.
func lockDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, "locks")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// lockKey hashes the working-tree path so two runs targeting the same tree
// collide on one lock file regardless of how the path was spelled.
func lockKey(workdir string) string {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		abs = workdir
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return hex.EncodeToString(sum[:8])
}

// AcquireLock takes an exclusive lock on workdir (a run's RepoRoot — the
// worktree for worktree runs, the main repo for branch/current runs). It
// prevents two overnight loops from racing the same git index and prevents a
// double-resume of the same run. Returns a release func; call it on exit.
//
// A lock left behind by a crashed process is detected via the recorded PID and
// stolen. On Windows there is no portable liveness probe, so a stale lock is
// kept (manual removal of ~/.bee/zzz/locks/<key>.lock required) rather than
// risk stealing a live one.
func AcquireLock(workdir, runID string) (func(), error) {
	dir, err := lockDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, lockKey(workdir)+".lock")
	release := func() { _ = os.Remove(path) }

	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n%s\n%s\n%s\n", os.Getpid(), runID, workdir, time.Now().UTC().Format(time.RFC3339))
			_ = f.Close()
			return release, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		pid, who := readLockHolder(path)
		if processAlive(pid) {
			return nil, fmt.Errorf("another zzz run holds %s (pid %d, run %s) — wait for it or remove %s", workdir, pid, who, path)
		}
		// stale lock from a dead process: remove and retry once.
		if rmErr := os.Remove(path); rmErr != nil {
			return nil, fmt.Errorf("stale lock %s could not be removed: %w", path, rmErr)
		}
	}
	return nil, fmt.Errorf("could not acquire lock for %s", workdir)
}

// readLockHolder parses the pid (line 1) and run id (line 2) from a lock file.
// Returns pid 0 when unreadable so the caller treats it as stale.
func readLockHolder(path string) (int, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, ""
	}
	lines := strings.Split(string(b), "\n")
	pid := 0
	if len(lines) > 0 {
		pid, _ = strconv.Atoi(strings.TrimSpace(lines[0]))
	}
	who := ""
	if len(lines) > 1 {
		who = strings.TrimSpace(lines[1])
	}
	return pid, who
}

// processAlive reports whether pid names a live process. On Windows there is no
// portable signal-0 probe, so it returns true (never steal). On unix, signal 0
// distinguishes alive (nil / EPERM) from dead (ESRCH).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		return true
	} else {
		return errors.Is(err, syscall.EPERM)
	}
}
