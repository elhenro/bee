// `bee fan` — spawn N parallel bees over a workload. Each bee owns its own
// Engine + session; results stream back via internal/hive's Pool.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/config"
	hivepkg "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/worktree"
)

// runFan parses flags, resolves the workload, builds N Engines, and runs
// them through hive.Pool. Stdout gets the final summary + merged transcript;
// stderr gets live state transitions.
func runFan(args []string) {
	fs := flag.NewFlagSet("fan", flag.ContinueOnError)
	defaultMax := runtime.NumCPU()
	if defaultMax > 8 {
		defaultMax = 8
	}
	maxConc := fs.Int("max", defaultMax, "max parallel bees (default min(8, NumCPU))")
	task := fs.String("task", "", "task description applied to each worker")
	per := fs.String("per", "file", "workload mode: file|line|count")
	count := fs.Int("count", 0, "number of workers when --per=count")
	model := fs.String("model", "", "override config default_model")
	providerName := fs.String("provider", "", "override config default_provider")
	sandboxScope := fs.String("sandbox", "", "override sandbox scope")
	isolated := fs.Bool("isolated", false, "give each worker its own git worktree (avoids write races)")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if strings.TrimSpace(*task) == "" {
		fmt.Fprintln(os.Stderr, "bee fan: --task is required")
		os.Exit(2)
	}

	workers, err := resolveWorkload(*per, *task, *count, fs.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee fan: %v\n", err)
		os.Exit(2)
	}
	if len(workers) == 0 {
		fmt.Fprintln(os.Stderr, "bee fan: no work to do")
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		if os.Getenv("BEE_TEST_PROVIDER") == "stub" {
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "bee fan: config: %v\n", err)
			os.Exit(1)
		}
	}
	applyOverrides(&cfg, *model, *providerName, *sandboxScope)

	prov, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee fan: provider: %v\n", err)
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	reg, err := buildTools(cwd, cfg, prov, storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee fan: tools: %v\n", err)
		os.Exit(1)
	}

	pool := hivepkg.NewPool(*maxConc)
	rolls := make([]*session.Rollout, 0, len(workers))
	worktrees := make([]*worktree.Worktree, 0, len(workers))
	defer func() {
		for _, r := range rolls {
			_ = r.Close()
		}
		for _, w := range worktrees {
			if err := w.Cleanup(); err != nil {
				fmt.Fprintf(os.Stderr, "bee fan: cleanup worktree: %v\n", err)
			}
		}
	}()

	for i, wspec := range workers {
		workerCwd := cwd
		workerReg := reg
		if *isolated {
			wt, err := worktree.Create(cwd, fmt.Sprintf("fan-w%d", i))
			if err != nil {
				fmt.Fprintf(os.Stderr, "bee fan: worker %s worktree: %v\n", wspec.name, err)
				os.Exit(1)
			}
			worktrees = append(worktrees, wt)
			workerCwd = wt.Path
			r2, err := buildTools(workerCwd, cfg, prov, storeDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bee fan: worker %s tools: %v\n", wspec.name, err)
				os.Exit(1)
			}
			workerReg = r2
		}
		eng, roll, err := newWorkerEngine(prov, workerReg, cfg, workerCwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee fan: build worker %s: %v\n", wspec.name, err)
			os.Exit(1)
		}
		rolls = append(rolls, roll)
		pool.Submit(&hivepkg.Worker{
			ID:     uuid.NewString(),
			Name:   wspec.name,
			Task:   wspec.task,
			Engine: eng,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	// progress watcher: prints state transitions to stderr until results close.
	resCh := pool.Run(ctx)
	doneCh := make(chan struct{})
	var collected []hivepkg.Result
	var mu sync.Mutex
	go func() {
		defer close(doneCh)
		for r := range resCh {
			mu.Lock()
			collected = append(collected, r)
			mu.Unlock()
			tag := "ok"
			if r.Err != nil {
				tag = "err"
			}
			fmt.Fprintf(os.Stderr, "[%s] %s done in %s\n",
				tag, r.Name, r.Ended.Sub(r.Started).Round(time.Millisecond))
		}
	}()
	<-doneCh

	mu.Lock()
	finalResults := collected
	mu.Unlock()

	fmt.Fprint(os.Stdout, hivepkg.Summary(finalResults))
	if merged := hivepkg.MergeText(finalResults); merged != "" {
		fmt.Fprintln(os.Stdout)
		fmt.Fprint(os.Stdout, merged)
	}

	for _, r := range finalResults {
		if r.Err != nil {
			os.Exit(1)
		}
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

// newWorkerEngine builds a fresh Engine with its own session rollout. Each
// bee gets isolated state — that's the whole point of fan-out. Memory is
// intentionally skipped here to keep the fan-out cheap; revisit if users
// want shared learning across workers.
func newWorkerEngine(prov llm.Provider, reg *tools.Registry, cfg config.Config, cwd string) (*loop.Engine, *session.Rollout, error) {
	sessID := uuid.NewString()
	roll, err := session.Open(sessID)
	if err != nil {
		return nil, nil, err
	}
	// suppress per-worker stdout streams; fan caller renders the merged
	// transcript at the end.
	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Sessions: roll,
		Cfg:      cfg,
		Cwd:      cwd,
		Stdout:   io.Discard,
	}
	return eng, roll, nil
}
