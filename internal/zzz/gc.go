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
// are always retained.
type PruneOpts struct {
	MaxAge     time.Duration
	KeepNewest int
}

// PruneResult lists what Prune deleted.
type PruneResult struct {
	RemovedRunIDs []string
	Errors        []error
}

// Prune sweeps ~/.bee/zzz/runs/ removing old terminal runs per opts. It does
// NOT touch worktrees — those have their own lifecycle (worktree --cleanup
// or git worktree prune). Active/running entries are always preserved.
func Prune(opts PruneOpts) PruneResult {
	var res PruneResult
	runs, err := ListRuns()
	if err != nil {
		res.Errors = append(res.Errors, err)
		return res
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
	now := time.Now().UTC()
	for i, r := range terminal {
		// retain the KeepNewest most-recent terminal runs unconditionally
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
