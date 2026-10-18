package main

import (
	"errors"
	"testing"

	hivepkg "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/worktree"
)

func TestSwarmWorkFailedSubtaskDiscardsItsWorktree(t *testing.T) {
	wts := []*worktree.Worktree{{Path: "a"}, {Path: "b"}}
	results := []hivepkg.Result{
		{},                          // worker 0
		{},                          // worker 1
		{Err: errors.New("failed")}, // worker 0 again
	}
	work := swarmWork(wts, results)
	if len(work) != 2 || work[0].ok || !work[1].ok {
		t.Fatalf("work = %+v, want worker 0 failed and worker 1 ok", work)
	}
}

func TestSwarmWorkNoWorktrees(t *testing.T) {
	if got := swarmWork(nil, []hivepkg.Result{{}}); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}
