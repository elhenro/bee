package main

import (
	"fmt"
	"os"
	"time"

	"github.com/elhenro/bee/internal/bgreg"
	"github.com/elhenro/bee/internal/zzz"
)

// startRun creates a brand-new run: new id, branch (or worktree), persisted
// meta + prompt. Validates the repo root and clean tree up front.
//
// The returned unlock holds the zzz lock of the tree the run drives. It is
// taken before the first change so a second zzz cannot move the running
// loop's branch or rewrite its meta.
func startRun(cfg zzz.Config) (r *zzz.Run, unlock func(), err error) {
	cwd, _ := os.Getwd()
	root, err := zzz.RepoRoot(cwd)
	if err != nil {
		return nil, nil, fmt.Errorf("not inside a git repo: %w", err)
	}
	id := zzz.NewID()
	// held lives apart from the named result: an error return nils unlock
	// before this defer runs
	held := func() {}
	if !cfg.Worktree {
		l, lerr := zzz.AcquireLock(root, id)
		if lerr != nil {
			return nil, nil, lerr
		}
		held = l
	}
	defer func() {
		if err != nil {
			held()
		}
	}()
	clean, err := zzz.IsClean(root)
	if err != nil {
		return nil, nil, err
	}
	if !clean {
		return nil, nil, fmt.Errorf("working tree is dirty — commit, stash, or discard before starting")
	}
	r = &zzz.Run{
		ID:        id,
		Objective: cfg.Objective,
		RepoRoot:  root,
		StartedAt: time.Now().UTC(),
		Status:    zzz.StatusRunning,
		PID:       os.Getpid(),
	}
	switch {
	case cfg.Worktree:
		wt, werr := zzz.WorktreeDir(id)
		if werr != nil {
			return nil, nil, werr
		}
		branch := "zzz/" + id
		if aerr := zzz.WorktreeAdd(root, wt, branch); aerr != nil {
			return nil, nil, fmt.Errorf("worktree add: %w", aerr)
		}
		// the worktree path is new, so nobody can hold it yet
		l, lerr := zzz.AcquireLock(wt, id)
		if lerr != nil {
			return nil, nil, lerr
		}
		held = l
		r.Mode = zzz.ModeWorktree
		r.Worktree = wt
		r.Branch = branch
		r.RepoRoot = wt
	case cfg.CurrentBranch:
		cur, cerr := zzz.CurrentBranch(root)
		if cerr != nil {
			return nil, nil, cerr
		}
		r.Mode = zzz.ModeCurrent
		r.Branch = cur
	default:
		branch := "zzz/" + id
		if berr := zzz.CreateBranchAndSwitch(root, branch); berr != nil {
			return nil, nil, fmt.Errorf("branch: %w", berr)
		}
		r.Mode = zzz.ModeBranch
		r.Branch = branch
	}
	if err = zzz.SavePrompt(id, cfg.Objective); err != nil {
		return nil, nil, err
	}
	if err = zzz.SaveMeta(r); err != nil {
		return nil, nil, err
	}
	return r, held, nil
}

// resumeRun loads existing run meta. id="" picks the most-recent run.
// Validates the recorded RepoRoot still exists and is a git working tree —
// without this, a resumed run can fail mid-iteration with cryptic git errors
// when an earlier `--cleanup` removed the worktree.
func resumeRun(id string) (r *zzz.Run, unlock func(), err error) {
	if id == "" {
		r, err = zzz.LatestRun()
		if err != nil {
			return nil, nil, err
		}
		if r == nil {
			return nil, nil, fmt.Errorf("no prior runs to resume")
		}
	} else {
		r, err = zzz.LoadMeta(id)
		if err != nil {
			return nil, nil, err
		}
	}
	if r.Status != zzz.StatusRunning && r.Status != zzz.StatusAborted {
		return nil, nil, fmt.Errorf("run %s is %s; nothing to resume", r.ID, r.Status)
	}
	if _, serr := os.Stat(r.RepoRoot); serr != nil {
		return nil, nil, fmt.Errorf("repo root %q missing — worktree may have been cleaned up", r.RepoRoot)
	}
	if _, gerr := zzz.RepoRoot(r.RepoRoot); gerr != nil {
		return nil, nil, fmt.Errorf("%q is not a git working tree anymore: %w", r.RepoRoot, gerr)
	}
	// lock before anything below reads the branch or rewrites the meta: a
	// second resume of a live run must change nothing.
	held, lerr := zzz.AcquireLock(r.RepoRoot, r.ID)
	if lerr != nil {
		return nil, nil, lerr
	}
	defer func() {
		if err != nil {
			held()
		}
	}()
	// current-branch runs commit to whatever branch is checked out; if the user
	// switched branches between stop and resume, resuming here would land the
	// run's commits on the wrong branch. refuse instead.
	if r.Mode == zzz.ModeCurrent && r.Branch != "" {
		cur, cerr := zzz.CurrentBranch(r.RepoRoot)
		if cerr != nil {
			return nil, nil, cerr
		}
		if cur != r.Branch {
			return nil, nil, fmt.Errorf("run %s committed to %q but %q is checked out — `git switch %s` before resuming", r.ID, r.Branch, cur, r.Branch)
		}
	}
	r.Status = zzz.StatusRunning
	r.PID = os.Getpid() // refresh so gc sees the live driver, not the old one
	if err = zzz.SaveMeta(r); err != nil {
		return nil, nil, fmt.Errorf("persist resume status: %w", err)
	}
	return r, held, nil
}

func zzzList() error {
	runs, err := zzz.ListRuns()
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Println("no zzz runs.")
		return nil
	}
	for _, r := range runs {
		fmt.Println(r.Summary())
	}
	return nil
}

func zzzCleanup(id string) error {
	r, err := zzz.LoadMeta(id)
	if err != nil {
		return err
	}
	if r.Mode != zzz.ModeWorktree || r.Worktree == "" {
		return fmt.Errorf("run %s is not a worktree run", id)
	}
	cwd, _ := os.Getwd()
	root, err := zzz.RepoRoot(cwd)
	if err != nil {
		return err
	}
	if err := zzz.WorktreeRemove(root, r.Worktree, true); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "removed worktree %s\n", r.Worktree)
	return nil
}

// runGC prunes terminal zzz runs + done bg sessions per opts, prints what
// was removed. With wantWorktrees, also calls `git worktree remove` for each
// pruned worktree run (must be invoked from the main repo, not a worktree).
// staleRunning>0 reaps "running" runs older than that threshold so crashed
// processes that never wrote a terminal status don't pile up forever.
func runGC(maxAge time.Duration, keep int, wantWorktrees bool, staleRunning time.Duration) {
	opts := zzz.PruneOpts{
		MaxAge:          maxAge,
		KeepNewest:      keep,
		StaleRunningAge: staleRunning,
		IncludeWorktree: wantWorktrees,
	}
	if wantWorktrees {
		cwd, _ := os.Getwd()
		if root, err := zzz.RepoRoot(cwd); err == nil {
			opts.MainRepoRoot = root
		} else {
			fmt.Fprintf(os.Stderr, "gc warn: --gc-worktrees needs to run inside a git repo (%v); skipping worktree removal.\n", err)
			opts.IncludeWorktree = false
		}
	}
	zRes := zzz.Prune(opts)
	bRes := bgreg.Prune(bgreg.PruneOpts{MaxAge: maxAge, KeepNewest: keep})
	list := func(title string, items []string) {
		fmt.Printf("%s: %d\n", title, len(items))
		for _, it := range items {
			fmt.Printf("  - %s\n", it)
		}
	}
	list("zzz runs pruned", zRes.RemovedRunIDs)
	if len(zRes.ReapedStaleRunIDs) > 0 {
		list("zzz runs reaped (stale running)", zRes.ReapedStaleRunIDs)
	}
	if len(zRes.RemovedWorktreePaths) > 0 {
		list("zzz worktrees removed", zRes.RemovedWorktreePaths)
	}
	list("bg sessions pruned", bRes.RemovedIDs)
	for _, e := range append(zRes.Errors, bRes.Errors...) {
		fmt.Fprintf(os.Stderr, "gc warn: %v\n", e)
	}
}
