package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/elhenro/bee/internal/approval"
	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/zzz"
	"github.com/elhenro/bee/internal/zzz/queen"
)

// runZzzQueen runs N headless worker bees in parallel, each in its own git
// worktree on the same objective, under one supervisor TUI ("queen"). The
// queen aggregates per-worker status and owns the kill switch: ctrl+d (or a
// second ctrl+c) cancels the shared ctx, aborting every worker mid-iteration.
func runZzzQueen(parentCtx context.Context, cancelAll context.CancelFunc, n int, cfg config.Config, prov llm.Provider, app approval.Approver, skillReg *skills.Registry, zCfg zzz.Config) {
	// every worker is isolated in its own worktree so N agents never race the
	// same git index. forced on regardless of the operator's flags.
	zCfg.Worktree = true

	// capture the base before any worktree branch is cut. workers fork from
	// this SHA, so it's the merge-base for judging each branch's full diff and
	// the point the consolidated review branch is compared against later.
	cwd, _ := os.Getwd()
	mainRoot, rootErr := zzz.RepoRoot(cwd)
	if rootErr != nil {
		fatalf("zzz: queen: not inside a git repo: %v", rootErr)
	}
	baseBranch, _ := zzz.CurrentBranch(mainRoot)
	baseSHA, _ := zzz.HeadSHA(mainRoot)
	queenID := zzz.NewID()

	runs := make([]*zzz.Run, 0, n)
	cleanups := make([]func(), 0, n)
	unlocks := make([]func(), 0, n)
	defer func() {
		for _, c := range cleanups {
			c()
		}
		for _, u := range unlocks {
			u()
		}
	}()

	type built struct {
		run    *zzz.Run
		runner zzz.Runner
	}
	var ready []built
	for i := 0; i < n; i++ {
		run, err := startRun(zCfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[queen] bee %d setup failed: %v\n", i+1, err)
			continue
		}
		unlock, lerr := zzz.AcquireLock(run.RepoRoot, run.ID)
		if lerr != nil {
			fmt.Fprintf(os.Stderr, "[queen] bee %d lock failed: %v\n", i+1, lerr)
			continue
		}
		unlocks = append(unlocks, unlock)
		eng, cleanup, err := buildZzzEngine(cfg, prov, app, skillReg, run.RepoRoot, io.Discard)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[queen] bee %d engine failed: %v\n", i+1, err)
			continue
		}
		cleanups = append(cleanups, cleanup)
		runs = append(runs, run)
		ready = append(ready, built{run: run, runner: zzz.NewLoopRunner(eng)})
	}
	if c, ok := app.(*approval.Cache); ok {
		defer c.Flush()
	}
	if len(ready) == 0 {
		fatalf("zzz: queen could not start any workers")
	}

	model := queen.New(runs, cancelAll)
	var wg sync.WaitGroup
	for i, b := range ready {
		wg.Add(1)
		go func(i int, b built) {
			defer wg.Done()
			// stopCh nil: graceful stop is delivered per-worker via the queen's
			// Steerable broadcast, not this external channel.
			err := zzz.Drive(parentCtx, nil, b.runner, zCfg, b.run, model.Worker(i))
			model.Done(i, b.run, err)
		}(i, b)
	}

	if err := model.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "zzz queen tui: %v\n", err)
	}
	cancelAll()
	model.Quit()
	wg.Wait()

	// altscreen restored — print a compact per-bee summary to stderr.
	for i, r := range runs {
		fmt.Fprintf(os.Stderr, "[queen] bee %d %s — %s (%d commits)\n", i+1, r.Status, r.StopCause, len(r.Commits))
	}

	pickWinnerAndConsolidate(cfg, prov, zCfg.Objective, runs, mainRoot, baseBranch, baseSHA, queenID)
	fmt.Fprintf(os.Stderr, "\n→ inspect runs under ~/.bee/zzz/runs/\n")
}

// pickWinnerAndConsolidate diffs every worker branch that produced commits,
// asks a fast model to judge the best, and points one consolidated review
// branch (zzz/queen-<id>) at it. The operator's base branch is never touched —
// they diff/merge the review branch against base manually later.
func pickWinnerAndConsolidate(cfg config.Config, prov llm.Provider, objective string, runs []*zzz.Run, mainRoot, baseBranch, baseSHA, queenID string) {
	ref := baseSHA
	if ref == "" {
		ref = baseBranch
	}
	var cands []queen.Candidate
	for i, r := range runs {
		if len(r.Commits) == 0 {
			continue
		}
		diff, err := zzz.DiffAgainst(r.RepoRoot, ref, 6000)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[queen] bee %d diff failed: %v\n", i+1, err)
			continue
		}
		cands = append(cands, queen.Candidate{Idx: i, Label: r.Branch, Diff: diff})
	}
	if len(cands) == 0 {
		fmt.Fprintf(os.Stderr, "[queen] no bee produced commits — nothing to consolidate.\n")
		return
	}

	// fresh ctx: the run ctx may already be canceled (operator force-quit) but
	// judging finished work is still worth one cheap call.
	jctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	v, err := queen.Judge(jctx, prov, config.FastModelOf(cfg), objective, cands)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[queen] judge note: %v (using fallback pick)\n", err)
	}
	if v.WinnerIdx < 0 {
		fmt.Fprintf(os.Stderr, "[queen] judge returned no winner.\n")
		return
	}
	winner := runs[v.WinnerIdx]
	fmt.Fprintf(os.Stderr, "[queen] winner: bee %d (%s) — %s\n", v.WinnerIdx+1, winner.Branch, v.Reason)

	reviewBranch := "zzz/queen-" + queenID
	if err := zzz.CreateBranchAt(mainRoot, reviewBranch, winner.Branch); err != nil {
		fmt.Fprintf(os.Stderr, "[queen] review branch create failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "[queen] winning branch is %s — merge it manually.\n", winner.Branch)
		return
	}
	cmp := baseBranch
	if cmp == "" || cmp == "HEAD" {
		cmp = baseSHA
	}
	fmt.Fprintf(os.Stderr, "[queen] review branch ready: %s\n", reviewBranch)
	fmt.Fprintf(os.Stderr, "         git diff %s..%s    # review against base\n", cmp, reviewBranch)
	fmt.Fprintf(os.Stderr, "         git switch %s && git merge %s    # accept\n", cmp, reviewBranch)
}
