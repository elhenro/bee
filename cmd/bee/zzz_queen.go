package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

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
	status := zzz.NewStatus(os.Stderr)
	for i, r := range runs {
		fmt.Fprintf(os.Stderr, "[queen] bee %d %s — %s (%d commits)\n", i+1, r.Status, r.StopCause, len(r.Commits))
		_ = status
	}
	fmt.Fprintf(os.Stderr, "\n→ inspect runs under ~/.bee/zzz/runs/\n")
}
