package zzz

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAcquireLock_ExclusiveAndRelease(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	dir := t.TempDir()

	rel, err := AcquireLock(dir, "run-1")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := AcquireLock(dir, "run-2"); err == nil {
		t.Fatal("second acquire should fail while the lock is held")
	}
	rel()
	rel2, err := AcquireLock(dir, "run-3")
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	rel2()
}

func TestAcquireLock_ChildOfHolderInherits(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	dir := t.TempDir()
	rel, err := AcquireLock(dir, "cron:job")
	if err != nil {
		t.Fatal(err)
	}
	defer rel()
	// the child process sees the parent's env entry and must not contend
	k, v, _ := strings.Cut(LockEnv(dir), "=")
	t.Setenv(k, v)
	childRel, err := AcquireLock(dir, "run-child")
	if err != nil {
		t.Fatalf("child of the holder must not fail: %v", err)
	}
	childRel()
	if _, err := AcquireLock(t.TempDir(), "other"); err != nil {
		t.Fatalf("env entry must only cover its own tree: %v", err)
	}
}

func TestLockKey_ResolvesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows")
	}
	dir := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if lockKey(dir) != lockKey(link) {
		t.Fatal("a symlinked checkout must share the lock key")
	}
}

func TestAcquireLock_StealsStale(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no portable process-liveness probe on windows; stale locks are kept")
	}
	t.Setenv("BEE_HOME", t.TempDir())
	dir := t.TempDir()
	ld, err := lockDir()
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(ld, lockKey(dir)+".lock")
	// a lock left by a long-dead pid must be stolen, not block forever.
	if err := os.WriteFile(lockPath, []byte("999999\nold-run\n"+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, err := AcquireLock(dir, "new-run")
	if err != nil {
		t.Fatalf("should steal stale lock from dead pid: %v", err)
	}
	rel()
}
