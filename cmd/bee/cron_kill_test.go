//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/elhenro/bee/internal/cron"
)

// cancelling a job must kill what its shell started, not only the shell.
func TestCronCancelKillsJobChildren(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	job := &cron.Job{Name: "j", Command: "sleep 60 & echo $! > " + pidFile + "; wait"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- newCronRunner().Run(ctx, job) }()

	var pid int
	for deadline := time.Now().Add(5 * time.Second); pid == 0 && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		b, _ := os.ReadFile(pidFile)
		pid, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	if pid == 0 {
		t.Fatal("job never started its child")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled job did not return")
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return
		}
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("job child %d survived cancel", pid)
}
