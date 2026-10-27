// Queen-and-workers orchestration.
//
// The Queen runs three or four phases:
//  1. decompose: ask Planner Runner to split the task into ≤8 sub-tasks (JSON
//     objects pairing role + task).
//  2. dispatch:  round-robin sub-tasks to Workers, fan out via goroutines.
//  3. review:    (optional) Reviewers inspect the working tree, a Verifier
//     re-checks every finding.
//  4. synthesize: hand all worker outputs (plus confirmed findings if any) back
//     to Planner for a final summary.
package hive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// MaxSubTasks caps how many sub-tasks the queen will accept from the planner.
// matches the prompt budget; defends against runaway plans.
const MaxSubTasks = 8

// Queen orchestrates a planner Runner and N worker Runners.
type Queen struct {
	Planner Runner
	Workers []Runner
	// Reviewers drive the verified review gate (decompose → … → review →
	// verify → synthesize). Each reviewer is paired to a ReviewDimension by
	// index and inspects the real working-tree changes. Their confirmed
	// findings are appended to the synthesize prompt. Verifier (optional)
	// re-checks every finding adversarially.
	Reviewers   []Runner
	Verifier    Runner
	MaxParallel int // 0 => len(Workers)
	// WorkerFor, when non-nil, supersedes the fixed Workers pool: dispatch calls
	// it once per sub-task to obtain a dedicated Runner (its own engine, often
	// rooted in an isolated worktree). The returned done func runs after that
	// sub-task finishes, with its error, so the caller can merge an isolated
	// tree back (or discard a failed one) and release resources. Because each
	// sub-task gets its own Runner, dispatch runs them concurrently up to
	// MaxParallel with no per-engine locking.
	WorkerFor func(idx int, sub SubTask) (r Runner, done func(err error), err error)
	// Hooks observes progress for a live UI. Zero value = no observation.
	Hooks Hooks
	// Pheromone is the shared state card every worker carries in its prompt
	// (goal, plan, sibling status). nil = Run allocates an in-memory one; set
	// NewPheromone(path) to mirror it to disk as well.
	Pheromone *Pheromone
	// Throttle holds each worker's start while the machine is oversubscribed.
	Throttle Throttle
	running  atomic.Int32 // workers past the throttle, for its sibling check
}

// QueenResult is the aggregate of one Queen.Run.
type QueenResult struct {
	Plan          []SubTask
	WorkerResults []Result
	Critique      string
	Findings      []Finding // verified review-gate findings (empty if gate not run)
	Final         string
}

// NewQueen returns a Queen ready to Run. MaxParallel defaults to len(workers).
func NewQueen(planner Runner, workers []Runner) *Queen {
	return &Queen{Planner: planner, Workers: workers, MaxParallel: len(workers)}
}

// Run executes the full decompose → dispatch → (review) → synthesize pipeline.
func (q *Queen) Run(ctx context.Context, task string) (QueenResult, error) {
	if q.Planner == nil {
		return QueenResult{}, errors.New("queen: planner is nil")
	}
	if len(q.Workers) == 0 && q.WorkerFor == nil {
		return QueenResult{}, errors.New("queen: no workers")
	}

	plan, err := q.decompose(ctx, task)
	if err != nil {
		return QueenResult{}, fmt.Errorf("queen: decompose: %w", err)
	}
	if len(plan) == 0 {
		// fallback: planner returned nothing useful; treat as single-task.
		plan = []SubTask{{Role: RoleBuilder, Task: task}}
	}
	if q.Pheromone == nil {
		q.Pheromone = NewPheromone("")
	}
	q.Pheromone.Reset(task, plan)
	fire(q.Hooks.OnPlan, plan)

	res := QueenResult{Plan: plan}
	if res.WorkerResults, err = q.dispatch(ctx, plan); err != nil {
		// dispatch only errors on ctx cancellation; worker failures are kept in
		// Result.Err and must not abort the run.
		return res, err
	}

	// review gate: confirmed findings feed synthesize via the critique string.
	if len(q.Reviewers) > 0 {
		if res.Findings, err = q.reviewAndVerify(ctx, task, plan, res.WorkerResults); err != nil {
			return res, fmt.Errorf("queen: review: %w", err)
		}
		res.Critique = renderFindings(res.Findings)
	}

	if q.Hooks.OnSynthesize != nil {
		q.Hooks.OnSynthesize()
	}
	if res.Final, err = q.synthesize(ctx, task, plan, res.WorkerResults, res.Critique); err != nil {
		return res, fmt.Errorf("queen: synthesize: %w", err)
	}
	return res, nil
}

// decompose asks the planner to split task into 2-8 independent sub-tasks
// emitted as a JSON array of {role, task} objects. Legacy string arrays are
// still accepted for backward compatibility.
func (q *Queen) decompose(ctx context.Context, task string) ([]SubTask, error) {
	prompt := fmt.Sprintf(
		"Decompose this task into 2-8 independent sub-tasks. "+
			"Return a JSON array of objects with shape "+
			`{"role": "<role>", "task": "<sub-task>"}. `+
			"Valid roles: %s. "+
			"Pick the role that best fits each sub-task. "+
			"Task: %s",
		strings.Join(roleNamesCSV(), ", "), task,
	)
	out, err := q.Planner.Run(ctx, prompt)
	if err != nil {
		return nil, err
	}
	plan := parseSubTasks(out.FinalText)
	if len(plan) > MaxSubTasks {
		plan = plan[:MaxSubTasks]
	}
	return plan, nil
}

// dispatch fans plan out across workers round-robin and waits for all to
// finish. An individual worker error does not abort siblings; it is recorded
// in that worker's Result.Err and the run proceeds. A hard error is returned
// only when the parent ctx is cancelled.
//
// With WorkerFor, each sub-task gets its own Runner from the factory, so they
// run concurrently up to MaxParallel with no per-engine lock. The factory's
// done func runs after each sub-task (merge-back / cleanup) and receives the
// worker's error so a failed run's isolated tree is discarded.
func (q *Queen) dispatch(ctx context.Context, plan []SubTask) ([]Result, error) {
	if q.WorkerFor != nil {
		parallel := q.MaxParallel
		if parallel <= 0 {
			parallel = len(plan)
		}
		return q.fanOut(ctx, plan, parallel, func(idx int, st SubTask) Result {
			runner, done, err := q.WorkerFor(idx, st)
			if err != nil {
				res := subResult(idx, st)
				res.Err = fmt.Errorf("spawn worker: %w", err)
				return res
			}
			res := q.runSub(ctx, idx, st, runner)
			if done != nil {
				done(res.Err) // merge-back / cleanup; failed runs are discarded
			}
			fire2(q.Hooks.OnWorkerDone, idx, res)
			return res
		})
	}
	parallel := q.MaxParallel
	if parallel <= 0 || parallel > len(q.Workers) {
		parallel = len(q.Workers)
	}
	// a worker engine is not safe to drive concurrently (RunWithContentDisplay
	// resets/writes unsynchronized per-run state, incl. maps → concurrent map
	// write panic). When the plan has more sub-tasks than workers, round-robin
	// reuses engines, so serialize the calls that map to each one. The semaphore
	// alone caps only TOTAL concurrency, not per-engine.
	locks := make([]sync.Mutex, len(q.Workers))
	return q.fanOut(ctx, plan, parallel, func(idx int, st SubTask) Result {
		widx := idx % len(q.Workers)
		locks[widx].Lock()
		defer locks[widx].Unlock()
		res := q.runSub(ctx, idx, st, q.Workers[widx])
		fire2(q.Hooks.OnWorkerDone, idx, res)
		return res
	})
}

// fanOut runs one goroutine per sub-task, at most parallel past the semaphore
// and the load throttle at a time, and collects their Results in plan order.
// A sub-task that never got to start carries the ctx error. Only ctx
// cancellation is returned as an error.
func (q *Queen) fanOut(ctx context.Context, plan []SubTask, parallel int, run func(idx int, st SubTask) Result) ([]Result, error) {
	results := make([]Result, len(plan))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup
	for i, sub := range plan {
		wg.Go(func() {
			release, err := q.Throttle.acquire(ctx, sem, &q.running)
			if err != nil {
				results[i] = subResult(i, sub)
				results[i].Err = err
				return
			}
			defer release()
			results[i] = run(i, sub)
		})
	}
	wg.Wait()
	return results, ctx.Err()
}

// synthesize hands all worker outputs back to the planner for a cohesive
// final summary. If critique is non-empty, it is appended verbatim.
func (q *Queen) synthesize(ctx context.Context, task string, plan []SubTask, results []Result, critique string) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b,
		"Synthesize these %d sub-task results into one cohesive summary. "+
			"Sub-tasks (with assigned roles) and results follow.\n\n"+
			"Original task: %s\n\n",
		len(results), task,
	)
	writePlanAndResults(&b, plan, results)
	if critique != "" {
		b.WriteString("### Critic review\n")
		b.WriteString(critique)
		b.WriteString("\n\n")
	}
	out, err := q.Planner.Run(ctx, b.String())
	if err != nil {
		return "", err
	}
	return out.FinalText, nil
}

// writePlanAndResults renders the plan + worker outputs into a shared format
// used by both review and synthesize.
func writePlanAndResults(b *strings.Builder, plan []SubTask, results []Result) {
	for i, r := range results {
		var st SubTask
		if i < len(plan) {
			st = plan[i]
		}
		role := string(st.Role)
		if role == "" {
			role = string(RoleBuilder)
		}
		fmt.Fprintf(b, "## Sub-task %d (role: %s)\n%s\n\n### Result\n", i+1, role, st.Task)
		if r.Err != nil {
			// a dead worker's death report is the only trace of its work.
			fmt.Fprintf(b, "(error: %v)\n", r.Err)
			if r.Report.Status != "" {
				b.WriteString(r.Report.Render())
				b.WriteString("\n")
			}
			b.WriteString("\n")
			continue
		}
		b.WriteString(strings.TrimSpace(r.Final))
		b.WriteString("\n\n")
	}
}
