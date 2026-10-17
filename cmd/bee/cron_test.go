package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/elhenro/bee/internal/cron"
	"github.com/elhenro/bee/internal/zzz"
)

func cronTestRegistry(t *testing.T, cmds map[string]string) *cron.Registry {
	t.Helper()
	reg := cron.NewRegistry()
	for _, name := range []string{"ok", "bad"} {
		if _, err := reg.Add(cron.JobSpec{Name: name, Schedule: "0 2 * * *", Command: cmds[name]}); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func TestCronRunOneExitCodes(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	reg := cronTestRegistry(t, map[string]string{"ok": "true", "bad": "exit 3"})
	if got := cronRunOne(reg, "ok"); got != 0 {
		t.Fatalf("ok job exit = %d, want 0", got)
	}
	if got := cronRunOne(reg, "bad"); got != 1 {
		t.Fatalf("failing job exit = %d, want 1", got)
	}
	if got := cronRunOne(reg, "missing"); got != 1 {
		t.Fatalf("unknown job exit = %d, want 1", got)
	}
}

// a held zzz worktree lock must make cron refuse to run, and release it after.
func TestCronRunnerRespectsZzzLock(t *testing.T) {
	t.Setenv("BEE_HOME", t.TempDir())
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	release, err := zzz.AcquireLock(wd, "zzz-run")
	if err != nil {
		t.Fatal(err)
	}
	job := &cron.Job{Name: "j", Command: "true"}
	err = newCronRunner().Run(context.Background(), job)
	if err == nil || !strings.Contains(err.Error(), "another zzz run holds") {
		t.Fatalf("run under held lock err = %v, want lock collision", err)
	}
	release()
	if err := newCronRunner().Run(context.Background(), job); err != nil {
		t.Fatalf("run after release: %v", err)
	}
	// cron must free the lock so a later zzz run can take it
	again, err := zzz.AcquireLock(wd, "zzz-run-2")
	if err != nil {
		t.Fatalf("lock not released by cron: %v", err)
	}
	again()
}
