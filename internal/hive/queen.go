// Queen-and-workers orchestration.
//
// The Queen runs three or four phases:
//  1. decompose: ask Planner Runner to split the task into ≤8 sub-tasks (JSON
//     objects pairing role + task).
//  2. dispatch:  round-robin sub-tasks to Workers, fan out via goroutines.
//  3. review:    (optional) hand worker outputs to Critic for a short critique.
//  4. synthesize: hand all worker outputs (plus critique if any) back to
//     Planner for a final summary.
//
// Pool from spawn.go (slice 4A) is preferred when available; this file uses
// direct goroutine fan-out so the queen still builds even if 4A is missing.
package hive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MaxSubTasks caps how many sub-tasks the queen will accept from the planner.
// matches the prompt budget; defends against runaway plans.
const MaxSubTasks = 8

// Hooks observes a Queen run as it progresses. Every callback is optional;
// nil callbacks are skipped, so existing callers (bee swarm) pass a zero
// Hooks and see unchanged behavior. Callbacks run on the goroutine driving
// the phase (OnWorkerStart/Done fire from worker goroutines) — keep them
// cheap and non-blocking (a channel send the UI drains is ideal).
type Hooks struct {
	OnPlan        func(plan []SubTask)
	OnWorkerStart func(idx int, sub SubTask)
	OnWorkerDone  func(idx int, res Result)
	OnReview      func(dimension string, claims []string)
	OnVerify      func(f Finding)
	OnCritique    func(critique string)
	OnSynthesize  func()
}

func (h Hooks) plan(p []SubTask) {
	if h.OnPlan != nil {
		h.OnPlan(p)
	}
}
func (h Hooks) workerStart(i int, s SubTask) {
	if h.OnWorkerStart != nil {
		h.OnWorkerStart(i, s)
	}
}
func (h Hooks) workerDone(i int, r Result) {
	if h.OnWorkerDone != nil {
		h.OnWorkerDone(i, r)
	}
}
func (h Hooks) review(dim string, claims []string) {
	if h.OnReview != nil {
		h.OnReview(dim, claims)
	}
}
func (h Hooks) verify(f Finding) {
	if h.OnVerify != nil {
		h.OnVerify(f)
	}
}
func (h Hooks) critique(c string) {
	if h.OnCritique != nil {
		h.OnCritique(c)
	}
}
func (h Hooks) synthesize() {
	if h.OnSynthesize != nil {
		h.OnSynthesize()
	}
}

// Queen orchestrates a planner Runner and N worker Runners. Critic is optional;
// when set, its output is appended to the synthesize prompt.
type Queen struct {
	Planner Runner
	Workers []Runner
	Critic  Runner
	// Reviewers drive the verified review gate (decompose → … → review →
	// verify → synthesize). When non-empty it supersedes Critic: each reviewer
	// is paired to a ReviewDimension by index and inspects the real working-tree
	// changes. Verifier (optional) re-checks every finding adversarially.
	Reviewers        []Runner
	Verifier         Runner
	ReviewDimensions []ReviewDimension // 0 => DefaultReviewDimensions()
	MaxParallel      int               // 0 => len(Workers)
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
	q.Hooks.plan(plan)

	results, err := q.dispatch(ctx, plan)
	if err != nil {
		// dispatch only errors on ctx cancellation; worker failures are kept in
		// Result.Err and must not abort the run.
		return QueenResult{Plan: plan, WorkerResults: results}, err
	}

	// review gate: the verified Reviewers path supersedes the legacy single
	// Critic. Both feed their summary into synthesize via the critique string.
	var critique string
	var findings []Finding
	switch {
	case len(q.Reviewers) > 0:
		findings, err = q.reviewAndVerify(ctx, task, plan, results)
		if err != nil {
			return QueenResult{Plan: plan, WorkerResults: results, Findings: findings}, fmt.Errorf("queen: review: %w", err)
		}
		critique = renderFindings(findings)
		if critique != "" {
			q.Hooks.critique(critique)
		}
	case q.Critic != nil:
		critique, err = q.review(ctx, task, plan, results)
		if err != nil {
			return QueenResult{Plan: plan, WorkerResults: results}, fmt.Errorf("queen: review: %w", err)
		}
		q.Hooks.critique(critique)
	}

	q.Hooks.synthesize()
	final, err := q.synthesize(ctx, task, plan, results, critique)
	if err != nil {
		return QueenResult{Plan: plan, WorkerResults: results, Critique: critique, Findings: findings}, fmt.Errorf("queen: synthesize: %w", err)
	}
	return QueenResult{Plan: plan, WorkerResults: results, Critique: critique, Findings: findings, Final: final}, nil
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
func (q *Queen) dispatch(ctx context.Context, plan []SubTask) ([]Result, error) {
	if q.WorkerFor != nil {
		return q.dispatchFactory(ctx, plan)
	}
	results := make([]Result, len(plan))
	parallel := q.MaxParallel
	if parallel <= 0 || parallel > len(q.Workers) {
		parallel = len(q.Workers)
	}
	sem := make(chan struct{}, parallel)
	// a worker engine is not safe to drive concurrently (RunWithContentDisplay
	// resets/writes unsynchronized per-run state, incl. maps → concurrent map
	// write panic). When the plan has more sub-tasks than workers, round-robin
	// reuses engines, so serialize the calls that map to each one. The semaphore
	// alone caps only TOTAL concurrency, not per-engine.
	locks := make([]sync.Mutex, len(q.Workers))

	var wg sync.WaitGroup

	for i, sub := range plan {
		widx := i % len(q.Workers)
		worker := q.Workers[widx]
		wg.Add(1)
		go func(idx, widx int, w Runner, st SubTask) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[idx] = Result{
					WorkerID: fmt.Sprintf("w%d", idx),
					Name:     fmt.Sprintf("worker-%d", idx),
					Task:     st.Task,
					Err:      ctx.Err(),
				}
				return
			}
			defer func() { <-sem }()
			locks[widx].Lock()
			defer locks[widx].Unlock()

			q.Hooks.workerStart(idx, st)
			started := time.Now().UTC()
			out, err := w.Run(ctx, st.Task)
			ended := time.Now().UTC()
			r := Result{
				WorkerID: fmt.Sprintf("w%d", idx),
				Name:     fmt.Sprintf("worker-%d", idx),
				Task:     st.Task,
				Started:  started,
				Ended:    ended,
			}
			if err != nil {
				r.Err = err
			} else {
				r.Final = out.FinalText
			}
			results[idx] = r
			q.Hooks.workerDone(idx, r)
		}(i, widx, worker, sub)
	}

	wg.Wait()
	// only ctx cancellation aborts; individual worker errors ride in Result.Err.
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	return results, nil
}

// dispatchFactory is the WorkerFor path: each sub-task gets its own Runner from
// the factory, so they run concurrently up to MaxParallel with no per-engine
// lock. The factory's done func runs after each sub-task (merge-back / cleanup)
// and receives the worker's error so a failed run's isolated tree is discarded.
func (q *Queen) dispatchFactory(ctx context.Context, plan []SubTask) ([]Result, error) {
	results := make([]Result, len(plan))
	parallel := q.MaxParallel
	if parallel <= 0 {
		parallel = len(plan)
	}
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup

	for i, sub := range plan {
		wg.Add(1)
		go func(idx int, st SubTask) {
			defer wg.Done()
			base := Result{
				WorkerID: fmt.Sprintf("w%d", idx),
				Name:     fmt.Sprintf("worker-%d", idx),
				Task:     st.Task,
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				base.Err = ctx.Err()
				results[idx] = base
				return
			}
			defer func() { <-sem }()

			runner, done, err := q.WorkerFor(idx, st)
			if err != nil {
				base.Err = fmt.Errorf("spawn worker: %w", err)
				results[idx] = base
				return
			}

			q.Hooks.workerStart(idx, st)
			base.Started = time.Now().UTC()
			out, runErr := runner.Run(ctx, st.Task)
			base.Ended = time.Now().UTC()
			if runErr != nil {
				base.Err = runErr
			} else {
				base.Final = out.FinalText
			}
			if done != nil {
				done(runErr) // merge-back / cleanup; failed runs are discarded
			}
			results[idx] = base
			q.Hooks.workerDone(idx, base)
		}(i, sub)
	}

	wg.Wait()
	if ctx.Err() != nil {
		return results, ctx.Err()
	}
	return results, nil
}

// review asks the Critic to read the plan + worker outputs and emit a short
// critique. Critic is opt-in via Queen.Critic.
func (q *Queen) review(ctx context.Context, task string, plan []SubTask, results []Result) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b,
		"Review this hive run. Read the original task, the plan, and each worker's result. "+
			"Emit a 1-2 paragraph critique: missed edge cases, weak spots, "+
			"and what's still uncertain. No fixes, no code.\n\n"+
			"Original task: %s\n\n",
		task,
	)
	writePlanAndResults(&b, plan, results)
	out, err := q.Critic.Run(ctx, b.String())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out.FinalText), nil
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
			fmt.Fprintf(b, "(error: %v)\n\n", r.Err)
			continue
		}
		b.WriteString(strings.TrimSpace(r.Final))
		b.WriteString("\n\n")
	}
}
