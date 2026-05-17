package bgreg

import (
	"sort"
	"time"
)

// PruneOpts mirrors zzz.PruneOpts: thresholds are AND-ed. Zero disables the
// matching threshold. Active/awaiting sessions are always preserved — only
// terminal (done, failed) sidecars are candidates.
type PruneOpts struct {
	MaxAge     time.Duration
	KeepNewest int
}

// PruneResult lists removed session IDs.
type PruneResult struct {
	RemovedIDs []string
	Errors     []error
}

// Prune sweeps ~/.bee/sessions/bg/ removing terminal Status sidecars per opts.
func Prune(opts PruneOpts) PruneResult {
	var res PruneResult
	all, err := List()
	if err != nil {
		res.Errors = append(res.Errors, err)
		return res
	}
	var terminal []Status
	for _, s := range all {
		if isTerminalState(s.State) {
			terminal = append(terminal, s)
		}
	}
	sort.Slice(terminal, func(i, j int) bool {
		return stamp(terminal[i]).After(stamp(terminal[j]))
	})
	now := time.Now().UTC()
	for i, s := range terminal {
		if opts.KeepNewest > 0 && i < opts.KeepNewest {
			continue
		}
		ts := stamp(s)
		if opts.MaxAge > 0 && now.Sub(ts) < opts.MaxAge {
			continue
		}
		if opts.MaxAge == 0 && opts.KeepNewest == 0 {
			continue
		}
		if err := Remove(s.SessionID); err != nil {
			res.Errors = append(res.Errors, err)
			continue
		}
		res.RemovedIDs = append(res.RemovedIDs, s.SessionID)
	}
	return res
}

func isTerminalState(s State) bool {
	return s == StateDone || s == StateFailed
}

func stamp(s Status) time.Time {
	if !s.FinishedAt.IsZero() {
		return s.FinishedAt
	}
	return s.UpdatedAt
}
