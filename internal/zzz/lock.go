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

	"github.com/elhenro/bee/internal/pidlock"
)

// lockKey hashes the working-tree path so two runs targeting the same tree
// collide on one lock file regardless of how the path was spelled. Symlinks
// are resolved so a symlinked checkout and its target share a key.
func lockKey(workdir string) string {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		abs = workdir
	}
	if real, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = real
	}
	sum := sha256.Sum256([]byte(filepath.Clean(abs)))
	return hex.EncodeToString(sum[:8])
}

// LockHeldEnv names the env var a lock holder sets for the children it
// spawns, so a child that would take the same lock (cron running `bee zzz`)
// knows its parent already owns the tree.
const LockHeldEnv = "BEE_ZZZ_LOCK_HELD"

// LockEnv returns the env entry to give a child process that runs while the
// caller holds the lock on workdir.
func LockEnv(workdir string) string { return LockHeldEnv + "=" + lockKey(workdir) }

// AcquireLock takes an exclusive lock on workdir (a run's RepoRoot — the
// worktree for worktree runs, the main repo for branch/current runs). It
// prevents two overnight loops from racing the same git index and prevents a
// double-resume of the same run. Returns a release func; call it on exit.
//
// The lock is a kernel lock on a file that is never unlinked (internal/
// pidlock), so a crashed holder frees it and two waiters cannot both win. A
// process whose parent already holds this tree's lock (see LockEnv) gets a
// no-op release instead of a failure.
func AcquireLock(workdir, runID string) (func(), error) {
	dir, err := zzzDir("locks")
	if err != nil {
		return nil, err
	}
	key := lockKey(workdir)
	if os.Getenv(LockHeldEnv) == key {
		return func() {}, nil
	}
	path := filepath.Join(dir, key+".lock")
	info := fmt.Sprintf("%s\n%s\n%s\n", runID, workdir, time.Now().UTC().Format(time.RFC3339))
	l, err := pidlock.Acquire(path, info)
	if held, ok := errors.AsType[*pidlock.HeldError](err); ok {
		// record: pid line, run id line, then the rest
		rec := append(strings.SplitN(held.Info, "\n", 3), "", "")
		pid, _ := strconv.Atoi(strings.TrimSpace(rec[0]))
		return nil, fmt.Errorf("another zzz run holds %s (pid %d, run %s) — wait for it to finish", workdir, pid, strings.TrimSpace(rec[1]))
	}
	if err != nil {
		return nil, err
	}
	return l.Release, nil
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
