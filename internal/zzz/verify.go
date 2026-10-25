package zzz

import (
	"context"
	"fmt"
	"time"

	"github.com/elhenro/bee/internal/gate"
	"github.com/elhenro/bee/internal/sandbox"
)

const (
	defaultMaxHops       = 3
	defaultVerifyTimeout = 600 * time.Second
)

// gateTree runs the verify gate on the iteration's uncommitted change and
// reports whether to commit. A red gate keeps the change for the next
// iteration to fix (IterRepair, one hop) until the hop budget is spent, then
// drops it (IterReset). No gate configured commits as before, unverified.
func gateTree(ctx context.Context, cfg Config, run *Run, r *IterationResult, ui UI) bool {
	if len(cfg.Verify) == 0 {
		return true
	}
	ui.SetPhase("verify")
	res, err := gate.Run(ctx, run.RepoRoot, cfg.Verify, gate.Opts{
		Timeout: orDefault(cfg.VerifyTimeout, defaultVerifyTimeout),
		Scope:   sandbox.Scope(cfg.SandboxScope),
	})
	if err != nil {
		r.Status = IterFailed
		r.Err = fmt.Errorf("verify: %w", err)
		run.Hops = 0
		rollbackTree(run.RepoRoot)
		return false
	}
	if res.OK {
		r.Verified = true
		run.Hops = 0
		return true
	}
	run.Hops++
	maxHops := orDefault(cfg.MaxHops, defaultMaxHops)
	if run.Hops > maxHops {
		ui.SetPhase("verify-dropped")
		r.Status = IterReset
		r.Err = fmt.Errorf("verify red %d times, change dropped", run.Hops)
		_ = AppendNote(run.ID, r.Iter, fmt.Sprintf("change dropped after %d red verifies, try a different approach", run.Hops), res.Summary())
		run.Hops = 0
		rollbackTree(run.RepoRoot)
		return false
	}
	ui.SetPhase("verify-red")
	r.Status = IterRepair
	subject := fmt.Sprintf("verify failed (hop %d/%d), change kept uncommitted: fix it before anything else", run.Hops, maxHops)
	_ = AppendNote(run.ID, r.Iter, subject, res.Summary())
	return false
}

// markIter tells the waggle miner how an iteration ended. Only a verified
// commit keeps its span; a repair leaves the span open so the eventual commit
// or drop decides it, and an ungated commit leaves it unverified so no
// mutating route is learned from untested work. A clean tree proves nothing
// about the shell calls that ran (a stash, a cleanup or a POST also leave the
// tree clean), so a no-change span is dropped, not verified.
func markIter(eng Runner, res IterationResult) {
	m, ok := eng.(outcomeMarker)
	if !ok {
		return
	}
	switch res.Status {
	case IterCommitted:
		if res.Verified {
			m.MarkOutcome(true)
		}
	case IterNoop, IterReset, IterFailed:
		m.MarkOutcome(false)
	}
}
