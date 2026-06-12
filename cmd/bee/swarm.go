// `bee swarm` subcommand: queen + N workers for one complex task.
//
// Wires loop.Engine instances (one planner, N workers) into hive.Queen,
// runs the decompose → dispatch → synthesize pipeline, prints progress.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/config"
	hivepkg "github.com/elhenro/bee/internal/hive"
	"github.com/elhenro/bee/internal/knowledge"
	"github.com/elhenro/bee/internal/llm"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/session"
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/worktree"
)

// runSwarm is invoked from main.go via swarm(args).
func runSwarm(args []string) {
	fs := flag.NewFlagSet("swarm", flag.ContinueOnError)
	nWorkers := fs.Int("workers", 4, "number of worker bees")
	plannerModel := fs.String("planner-model", "", "override planner model id")
	workerModel := fs.String("worker-model", "", "override worker model id")
	isolated := fs.Bool("isolated", false, "give each worker its own git worktree (avoids write races)")
	triage := fs.Bool("triage", true, "skip the hive on small/clear tasks (one streaming turn instead of plan+workers+review)")
	noTriage := fs.Bool("no-triage", false, "force the full hive even when triage says simple")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *nWorkers < 1 {
		fmt.Fprintln(os.Stderr, "bee swarm: --workers must be ≥1")
		os.Exit(2)
	}

	task, err := resolveUserMessage(fs.Args(), os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee swarm: %v\n", err)
		os.Exit(2)
	}

	cfg, err := config.Load()
	if err != nil {
		if os.Getenv("BEE_TEST_PROVIDER") == "stub" {
			cfg = config.Defaults()
		} else {
			fmt.Fprintf(os.Stderr, "bee swarm: config: %v\n", err)
			os.Exit(1)
		}
	}

	prov, err := buildProvider(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee swarm: provider: %v\n", err)
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	storeDir, _ := knowledge.StoreDir()
	plannerReg, err := buildTools(cwd, cfg, prov, storeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee swarm: tools: %v\n", err)
		os.Exit(1)
	}
	skillReg := skills.NewRegistry()
	_ = skillReg.Load(skills.BaseDir())

	// planner: larger model if user supplied one
	plannerCfg := cfg
	if *plannerModel != "" {
		plannerCfg.DefaultModel = *plannerModel
	}
	planner, plannerSess, err := newEngine(prov, plannerReg, skillReg, plannerCfg, cwd, io.Discard, "planner")
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee swarm: planner session: %v\n", err)
		os.Exit(1)
	}
	defer plannerSess.Close()

	// triage: a cheap one-token classifier decides if the task is small/clear
	// enough to skip the full hive. On "simple" we run a single streaming turn
	// on the planner engine — no decompose, no workers, no review. Saves a
	// full planner+worker+synth call on every trivial task. Ambiguity (and
	// any classifier error) falls through to the hive, so a risky change is
	// never under-reviewed. --no-triage forces the full hive.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if shortCircuit, scErr := maybeTriageShortCircuit(ctx, prov, cfg.DefaultModel, *triage && !*noTriage, planner.Run, task, os.Stdout); shortCircuit {
		if scErr != nil {
			os.Exit(1)
		}
		return
	}

	// workers: same provider, possibly smaller model
	workerCfg := cfg
	if *workerModel != "" {
		workerCfg.DefaultModel = *workerModel
	}
	workers := make([]hivepkg.Runner, 0, *nWorkers)
	closers := make([]io.Closer, 0, *nWorkers)
	worktrees := make([]*worktree.Worktree, 0, *nWorkers)
	defer func() {
		for _, c := range closers {
			_ = c.Close()
		}
		for _, w := range worktrees {
			if err := w.Cleanup(); err != nil {
				fmt.Fprintf(os.Stderr, "bee swarm: cleanup worktree: %v\n", err)
			}
		}
	}()
	for i := 0; i < *nWorkers; i++ {
		workerCwd := cwd
		workerReg := plannerReg
		if *isolated {
			wt, err := worktree.Create(cwd, fmt.Sprintf("swarm-w%d", i))
			if err != nil {
				fmt.Fprintf(os.Stderr, "bee swarm: worker %d worktree: %v\n", i, err)
				os.Exit(1)
			}
			worktrees = append(worktrees, wt)
			workerCwd = wt.Path
			// rebuild the registry so file-rooted tools (grep, find, ls,
			// write, edit_diff) target the isolated tree instead of cwd.
			reg, err := buildTools(workerCwd, cfg, prov, storeDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "bee swarm: worker %d tools: %v\n", i, err)
				os.Exit(1)
			}
			workerReg = reg
		}
		eng, sess, err := newEngine(prov, workerReg, skillReg, workerCfg, workerCwd, io.Discard, fmt.Sprintf("worker-%d", i))
		if err != nil {
			fmt.Fprintf(os.Stderr, "bee swarm: worker %d session: %v\n", i, err)
			os.Exit(1)
		}
		workers = append(workers, eng)
		closers = append(closers, sess)
	}

	q := hivepkg.NewQueen(planner, workers)

	fmt.Fprintln(os.Stderr, "swarm: decomposing task...")
	res, err := q.Run(ctx, task)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bee swarm: %v\n", err)
		printQueenResult(os.Stdout, res)
		os.Exit(1)
	}
	printQueenResult(os.Stdout, res)
}

// newEngine builds a loop.Engine + its session rollout. Returns the rollout
// so the caller can Close it at end of program.
func newEngine(
	prov llm.Provider,
	reg *tools.Registry,
	skillReg *skills.Registry,
	cfg config.Config,
	cwd string,
	stdout io.Writer,
	label string,
) (*loop.Engine, *session.Rollout, error) {
	sessID := uuid.NewString()
	roll, err := session.Open(sessID)
	if err != nil {
		return nil, nil, err
	}
	_ = label // session id is the canonical handle; label is debug-only
	eng := &loop.Engine{
		Provider: prov,
		Tools:    reg,
		Skills:   skillReg,
		Cfg:      cfg,
		Cwd:      cwd,
		Sessions: roll,
		Stdout:   stdout,
	}
	return eng, roll, nil
}

func printQueenResult(w io.Writer, r hivepkg.QueenResult) {
	if len(r.Plan) > 0 {
		fmt.Fprintln(w, "## Plan")
		for i, p := range r.Plan {
			fmt.Fprintf(w, "%d. [%s] %s\n", i+1, p.Role, p.Task)
		}
		fmt.Fprintln(w)
	}
	if len(r.WorkerResults) > 0 {
		fmt.Fprintln(w, "## Worker results")
		for i, wr := range r.WorkerResults {
			fmt.Fprintf(w, "### Worker %d — %s\n", i+1, wr.Task)
			if wr.Err != nil {
				fmt.Fprintf(w, "error: %v\n\n", wr.Err)
				continue
			}
			fmt.Fprintln(w, wr.Final)
			fmt.Fprintln(w)
		}
	}
	if r.Final != "" {
		fmt.Fprintln(w, "## Synthesis")
		fmt.Fprintln(w, r.Final)
	}
}

// printTriageShortCircuit emits the single-turn output for a triage=simple
// pass. Headers match printQueenResult's shape so downstream consumers
// (jq, scripts) see the same fields whether the hive ran or not.
func printTriageShortCircuit(w io.Writer, r loop.RunResult, err error) {
	if err != nil {
		fmt.Fprintf(w, "## Single turn (triage=simple)\nerror: %v\n", err)
		return
	}
	if r.FinalText != "" {
		fmt.Fprintln(w, "## Single turn (triage=simple)")
		fmt.Fprintln(w, r.FinalText)
	}
}

// swarmPlannerRun is the signature of loop.Engine.Run — broken out as a func
// type so the triage path is unit-testable without standing up a full engine
// (provider, tools, skills, session, cwd) for every test.
type swarmPlannerRun func(ctx context.Context, task string) (loop.RunResult, error)

// maybeTriageShortCircuit runs the triage classifier and, on "simple", runs a
// single streaming turn via plannerRun. Returns (true, _) when the caller
// should exit (hive skipped); (false, nil) to continue with the full hive
// (task was complex, classifier errored, or triage disabled).
//
// Extracted from runSwarm so the routing decision is unit-testable without
// spinning up the full swarm wiring (config, workers, worktrees, etc).
func maybeTriageShortCircuit(
	ctx context.Context,
	prov llm.Provider,
	model string,
	enabled bool,
	plannerRun swarmPlannerRun,
	task string,
	out io.Writer,
) (bool, error) {
	if !enabled {
		return false, nil
	}
	triageCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !hivepkg.TriageSimple(triageCtx, prov, model, task) {
		return false, nil
	}
	fmt.Fprintln(os.Stderr, "swarm: triage=simple — running single turn, no hive")
	res, err := plannerRun(ctx, task)
	printTriageShortCircuit(out, res, err)
	return true, err
}
