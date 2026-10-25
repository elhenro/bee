// `bee fan` — spawn N parallel bees over a workload. Each bee owns its own
// Engine + session; results stream back via internal/hive's Pool.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/config"
	hivepkg "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/worktree"
)

// runFan parses flags, resolves the workload, builds N Engines, and runs
// them through hive.Pool. Stdout gets the final summary + merged transcript;
// stderr gets live state transitions.
func runFan(args []string) {
	fs := flag.NewFlagSet("fan", flag.ContinueOnError)
	maxConc := fs.Int("max", min(runtime.NumCPU(), 8), "max parallel bees (default min(8, NumCPU))")
	task := fs.String("task", "", "task description applied to each worker")
	per := fs.String("per", "file", "workload mode: file|line|count")
	count := fs.Int("count", 0, "number of workers when --per=count")
	model := fs.String("model", "", "override config default_model")
	providerName := fs.String("provider", "", "override config default_provider")
	sandboxScope := fs.String("sandbox", "", "override sandbox scope")
	safe := fs.Bool("safe", false, "shorthand for --sandbox workspace-write: confine writes to cwd+tmp, block network")
	isolated := fs.Bool("isolated", false, "give each worker its own git worktree (avoids write races)")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if strings.TrimSpace(*task) == "" {
		exitf(2, "bee fan: --task is required")
	}

	workers, err := resolveWorkload(*per, *task, *count, fs.Args())
	if err != nil {
		exitf(2, "bee fan: %v", err)
	}
	if len(workers) == 0 {
		exitf(2, "bee fan: no work to do")
	}

	cfg, err := loadConfig(*model, *providerName, resolveSafe(*sandboxScope, *safe, ""))
	if err != nil {
		exitf(1, "bee fan: config: %v", err)
	}

	prov, err := buildProvider(cfg)
	if err != nil {
		exitf(1, "bee fan: provider: %v", err)
	}
	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	reg, err := buildTools(cwd, cfg, prov, storeDir)
	if err != nil {
		exitf(1, "bee fan: tools: %v", err)
	}

	code := func() int {
		pool := hivepkg.NewPool(*maxConc)
		pool.Throttle = cliThrottle("fan", cfg)
		// pheromone card: the shared task heads it, one row per target, mirrored
		// so a second terminal can watch a wide fan-out.
		pool.Goal = *task
		if path, perr := hivepkg.PheromonePath(uuid.NewString()); perr == nil {
			pool.Pheromone = hivepkg.NewPheromone(path)
			fmt.Fprintf(os.Stderr, "fan: pheromone card at %s\n", path)
		}
		rolls := make([]*session.Rollout, 0, len(workers))
		worktrees := make([]*worktree.Worktree, 0, len(workers))
		wtByWorker := map[string]*worktree.Worktree{}
		kept := map[*worktree.Worktree]bool{}
		defer func() {
			for _, r := range rolls {
				_ = r.Close()
			}
			cleanupWorktrees("fan", worktrees, kept)
		}()

		for i, wspec := range workers {
			workerCwd := cwd
			workerReg := reg
			var workerWT *worktree.Worktree
			if *isolated {
				wt, err := worktree.Create(cwd, fmt.Sprintf("fan-w%d", i))
				if err != nil {
					return failf(1, "bee fan: worker %s worktree: %v", wspec.name, err)
				}
				worktrees = append(worktrees, wt)
				workerWT = wt
				workerCwd = wt.Path
				r2, err := buildTools(workerCwd, cfg, prov, storeDir)
				if err != nil {
					return failf(1, "bee fan: worker %s tools: %v", wspec.name, err)
				}
				workerReg = r2
			}
			eng, roll, err := newEngine(prov, workerReg, nil, cfg, workerCwd)
			if err != nil {
				return failf(1, "bee fan: build worker %s: %v", wspec.name, err)
			}
			rolls = append(rolls, roll)
			id := uuid.NewString()
			if workerWT != nil {
				wtByWorker[id] = workerWT
			}
			pool.Submit(&hivepkg.Worker{
				ID:     id,
				Name:   wspec.name,
				Task:   wspec.task,
				Engine: eng,
			})
		}

		sig, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ctx, cancel := context.WithTimeout(sig, 20*time.Minute)
		defer cancel()

		// progress: prints state transitions to stderr until results close.
		var finalResults []hivepkg.Result
		for r := range pool.Run(ctx) {
			finalResults = append(finalResults, r)
			tag := "ok"
			if r.Err != nil {
				tag = "err"
			}
			fmt.Fprintf(os.Stderr, "[%s] %s done in %s\n",
				tag, r.Name, r.Ended.Sub(r.Started).Round(time.Millisecond))
		}

		fmt.Fprint(os.Stdout, hivepkg.Summary(finalResults))
		if merged := hivepkg.MergeText(finalResults); merged != "" {
			fmt.Fprintln(os.Stdout)
			fmt.Fprint(os.Stdout, merged)
		}

		code := 0
		work := make([]isolatedWork, 0, len(finalResults))
		for _, r := range finalResults {
			if r.Err != nil {
				code = 1
			}
			work = append(work, isolatedWork{name: r.Name, wt: wtByWorker[r.WorkerID], ok: r.Err == nil})
		}
		kept = mergeIsolated("fan", work)
		if len(kept) > 0 {
			code = 1
		}
		return code
	}()
	if code != 0 {
		os.Exit(code)
	}
}

// workerSpec is one logical unit of work. The pool turns it into a hivepkg.Worker.
type workerSpec struct {
	name string
	task string
}

// resolveWorkload turns CLI flags into a list of worker specs. file mode
// expands each positional glob into matching files; count mode creates N
// copies of the task.
func resolveWorkload(mode, task string, count int, positional []string) ([]workerSpec, error) {
	switch mode {
	case "file":
		if len(positional) == 0 {
			return nil, fmt.Errorf("--per=file needs at least one glob pattern")
		}
		files, err := expandGlobs(positional)
		if err != nil {
			return nil, err
		}
		out := make([]workerSpec, 0, len(files))
		for _, f := range files {
			out = append(out, workerSpec{
				name: filepath.Base(f),
				task: fmt.Sprintf("%s on file %s", task, f),
			})
		}
		return out, nil
	case "count":
		if count <= 0 {
			return nil, fmt.Errorf("--per=count needs --count > 0")
		}
		out := make([]workerSpec, count)
		for i := 0; i < count; i++ {
			out[i] = workerSpec{
				name: fmt.Sprintf("bee-%d", i+1),
				task: fmt.Sprintf("%s [#%d]", task, i+1),
			}
		}
		return out, nil
	case "line":
		return nil, fmt.Errorf("--per=line not yet implemented")
	default:
		return nil, fmt.Errorf("unknown --per mode %q", mode)
	}
}

// expandGlobs runs each pattern through filepath.Glob and dedupes.
func expandGlobs(patterns []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", p, err)
		}
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}
	return out, nil
}

// cliThrottle holds hive worker starts on an oversubscribed machine and says
// so on stderr.
func cliThrottle(cmd string, cfg config.Config) hivepkg.Throttle {
	ratio := cfg.Swarm.MaxLoadRatio
	return hivepkg.Throttle{
		MaxLoadRatio: ratio,
		OnHold: func(l float64) {
			fmt.Fprintf(os.Stderr, "%s: load %.1f/core over %.1f, holding a worker\n", cmd, l, ratio)
		},
	}
}
