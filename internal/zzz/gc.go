package zzz

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PruneOpts controls run-directory garbage collection. A run is removed when
// it satisfies BOTH thresholds — older than MaxAge AND beyond the KeepNewest
// most recent. Zero values disable the matching threshold:
//   - MaxAge=0 → age never triggers; only excess beyond KeepNewest removed.
//   - KeepNewest=0 → count never triggers; only age-based removal.
//
// Only terminal runs (completed, failed, aborted) are candidates. Active runs
// are always retained, except runs stuck in "running" past StaleRunningAge —
// those are reaped because they almost always indicate a crashed process that
// never persisted a terminal status.
type PruneOpts struct {
	MaxAge          time.Duration
	KeepNewest      int
	StaleRunningAge time.Duration // 0 → never reap running runs
	// IncludeWorktree triggers `git worktree remove` per pruned worktree run.
	// MainRepoRoot must be set to the main repo (not a worktree) — git refuses
	// to remove a worktree from itself.
	IncludeWorktree bool
	MainRepoRoot    string
}

// PruneResult lists what Prune deleted.
type PruneResult struct {
	RemovedRunIDs        []string
	ReapedStaleRunIDs    []string
	RemovedWorktreePaths []string
	Errors               []error
}

// Prune sweeps ~/.bee/zzz/runs/ removing old terminal runs per opts. With
// IncludeWorktree, also removes the associated git worktree for each pruned
// worktree-mode run. StaleRunningAge>0 also reaps "running" runs older than
// that threshold (process crashed before persisting a terminal status).
func Prune(opts PruneOpts) PruneResult {
	var res PruneResult
	runs, err := ListRuns()
	if err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}
	now := time.Now().UTC()

	// reap stale running entries first so they fall into the terminal bucket
	// below if also old enough by MaxAge/KeepNewest.
	if opts.StaleRunningAge > 0 {
		for _, r := range runs {
			if r.Status != StatusRunning {
				continue
			}
			stamp := r.StartedAt
			if now.Sub(stamp) < opts.StaleRunningAge {
				continue
			}
			r.Status = StatusAborted
			r.StopCause = "reaped by gc (stale running)"
			r.EndedAt = now
			if err := SaveMeta(r); err != nil {
				res.Errors = append(res.Errors, err)
				continue
			}
			res.ReapedStaleRunIDs = append(res.ReapedStaleRunIDs, r.ID)
		}
		// refresh listing so newly-reaped runs are seen as terminal below.
		runs, _ = ListRuns()
	}

	// runs already sorted newest-first by ListRuns.
	var terminal []*Run
	for _, r := range runs {
		if isTerminal(r.Status) {
			terminal = append(terminal, r)
		}
	}
	sort.Slice(terminal, func(i, j int) bool {
		return terminal[i].EndedAt.After(terminal[j].EndedAt)
	})
	for i, r := range terminal {
		if opts.KeepNewest > 0 && i < opts.KeepNewest {
			continue
		}
		stamp := r.EndedAt
		if stamp.IsZero() {
			stamp = r.StartedAt
		}
		if opts.MaxAge > 0 && now.Sub(stamp) < opts.MaxAge {
			continue
		}
		if opts.MaxAge == 0 && opts.KeepNewest == 0 {
			continue
		}
		// remove worktree first so a `git worktree remove` failure doesn't
		// orphan the metadata that would have told us where to look.
		if opts.IncludeWorktree && r.Mode == ModeWorktree && r.Worktree != "" && opts.MainRepoRoot != "" {
			if err := WorktreeRemove(opts.MainRepoRoot, r.Worktree, true); err != nil {
				res.Errors = append(res.Errors, err)
			} else {
				res.RemovedWorktreePaths = append(res.RemovedWorktreePaths, r.Worktree)
			}
		}
		if err := removeRunDir(r.ID); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		res.RemovedRunIDs = append(res.RemovedRunIDs, r.ID)
	}
	return res
}

func isTerminal(s string) bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusAborted:
		return true
	}
	return false
}

func removeRunDir(id string) error {
	home, err := HomeDir()
	if err != nil {
		return err
	}
	return os.RemoveAll(filepath.Join(home, "runs", id))
}
