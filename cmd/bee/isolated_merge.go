package main

import (
	"fmt"
	"os"

	hivepkg "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/worktree"
)

// swarmWork maps queen results onto worktrees. Sub-task i ran on worker
// i%len(wts); a worktree is ok when all of its sub-tasks succeeded.
func swarmWork(wts []*worktree.Worktree, results []hivepkg.Result) []isolatedWork {
	if len(wts) == 0 {
		return nil
	}
	work := make([]isolatedWork, len(wts))
	for i, wt := range wts {
		work[i] = isolatedWork{name: fmt.Sprintf("worker-%d", i), wt: wt, ok: true}
	}
	for i, r := range results {
		if r.Err != nil {
			work[i%len(wts)].ok = false
		}
	}
	return work
}

// isolatedWork pairs a worker's worktree with whether the worker finished
// without error. Failed workers are discarded, not merged.
type isolatedWork struct {
	name string
	wt   *worktree.Worktree
	ok   bool
}

// mergeIsolated applies each successful worker's worktree back onto the
// source tree, one at a time (the git index is per-repo). It returns the
// worktrees whose merge failed: the caller must not Cleanup those, they hold
// the only copy of the worker's edits.
func mergeIsolated(cmd string, work []isolatedWork) map[*worktree.Worktree]bool {
	kept := map[*worktree.Worktree]bool{}
	for _, w := range work {
		if !w.ok || w.wt == nil {
			continue
		}
		if err := w.wt.MergeBack(); err != nil {
			kept[w.wt] = true
			fmt.Fprintf(os.Stderr, "bee %s: merge %s failed, edits kept in %s: %v\n", cmd, w.name, w.wt.Path, err)
		}
	}
	return kept
}

// cleanupWorktrees removes every worktree except the kept ones.
func cleanupWorktrees(cmd string, wts []*worktree.Worktree, kept map[*worktree.Worktree]bool) {
	for _, w := range wts {
		if kept[w] {
			continue
		}
		if err := w.Cleanup(); err != nil {
			fmt.Fprintf(os.Stderr, "bee %s: cleanup worktree: %v\n", cmd, err)
		}
	}
}
