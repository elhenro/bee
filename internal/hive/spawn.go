// Fan-out orchestrator. Spawns up to MaxConcurrency workers in parallel,
// each driving its own Engine via the Runner contract. Results stream out
// on a channel as workers finish; cancellation propagates through ctx.
package hive

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Pool manages a bounded set of concurrent Workers.
type Pool struct {
	MaxConcurrency int

	// Goal and Pheromone shape the card every worker carries (pheromone.go).
	// Goal heads the card; under one, rows show each worker's Name, since the
	// task is then the goal applied per target and only the name tells rows
	// apart. A nil Pheromone keeps the card in memory; one built with a
	// PheromonePath mirrors it to disk.
	Goal      string
	Pheromone *Pheromone
	// Throttle holds each worker's start while the machine is oversubscribed.
	Throttle Throttle
	running  atomic.Int32 // workers past the throttle, for its sibling check

	mu      sync.Mutex
	Workers []*Worker
}

// NewPool builds a Pool with the given concurrency cap. A non-positive cap
// is treated as 1 — refusing to run any work would surprise callers.
func NewPool(max int) *Pool {
	if max < 1 {
		max = 1
	}
	return &Pool{MaxConcurrency: max}
}

// Submit registers a Worker with the pool.
// Safe to call before Run; calling after Run is a no-op for that worker
// because the dispatcher has already iterated the snapshot.
func (p *Pool) Submit(w *Worker) {
	if w == nil {
		return
	}
	p.mu.Lock()
	p.Workers = append(p.Workers, w)
	p.mu.Unlock()
}

// snapshotWorkers returns a shallow copy of the Worker pointer slice for safe
// iteration without holding the lock during long-running goroutines.
func (p *Pool) snapshotWorkers() []*Worker {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.Workers)
}

// Run dispatches submitted workers up to MaxConcurrency at a time. Each
// worker's Engine.Run is invoked with a context derived from ctx. Results
// land on the returned channel as workers finish; the channel closes once
// all workers are done (including those skipped by cancellation).
func (p *Pool) Run(ctx context.Context) <-chan Result {
	workers := p.snapshotWorkers()
	results := make(chan Result, len(workers))

	// one card for the fan-out: no queen plan, so each worker is a row and
	// siblings see each other's reports as they land.
	ph := p.Pheromone
	if ph == nil {
		ph = NewPheromone("")
	}
	plan := make([]SubTask, len(workers))
	for i, w := range workers {
		plan[i] = SubTask{Task: p.rowLabel(w)}
	}
	ph.Reset(p.Goal, plan)

	sem := make(chan struct{}, p.MaxConcurrency)
	var wg sync.WaitGroup

	for i, w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// honor cancellation before running: avoids starting late workers
			// after the caller has already given up.
			release, err := p.Throttle.acquire(ctx, sem, &p.running)
			if err != nil {
				results <- canceledResult(w, err)
				return
			}
			defer release()

			results <- runOne(ctx, w, i, ph)
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()
	return results
}

// rowLabel is a worker's row on the card: its Name under a shared Goal, else
// its full Task.
func (p *Pool) rowLabel(w *Worker) string {
	if p.Goal != "" && w.Name != "" {
		return w.Name
	}
	return w.Task
}

// runOne executes a single worker's Engine.Run, capturing timing, the final
// assistant text and the exit report, and keeps the worker's row on the
// shared card current. A nil Engine is a programmer error and produces an
// error Result rather than panicking. Tests inject a Runner via runnerHook
// (see spawn_test.go).
func runOne(ctx context.Context, w *Worker, idx int, ph *Pheromone) Result {
	r := Result{
		WorkerID: w.ID,
		Name:     w.Name,
		Task:     w.Task,
		Started:  time.Now().UTC(),
	}
	var runner Runner
	if runnerHook != nil {
		runner = runnerHook(w)
	}
	if runner == nil && w.Engine != nil {
		runner = w.Engine
	}
	if runner == nil {
		r.Err = errNilEngine
		r.Ended = time.Now().UTC()
		return r
	}
	if t, ok := runner.(Tailed); ok {
		t.SetPromptTail(ph.Tail(idx, SubTask{Task: w.Task}))
	}
	ph.Start(idx)
	run, err := runner.Run(ctx, w.Task)
	r.Ended = time.Now().UTC()
	r.Final = run.FinalText
	if err != nil {
		r.Err = err
	}
	r.Report = reportFor(ctx, runner, run, err)
	ph.Finish(idx, r)
	return r
}

// runnerHook is a test-only seam: it supplies a stub Runner in place of the
// Worker's Engine. Production leaves it nil.
var runnerHook func(*Worker) Runner

func canceledResult(w *Worker, err error) Result {
	now := time.Now().UTC()
	return Result{
		WorkerID: w.ID,
		Name:     w.Name,
		Task:     w.Task,
		Err:      err,
		Started:  now,
		Ended:    now,
	}
}

// errNilEngine is returned when a Worker without an Engine is dispatched.
// Sentinel so tests can identify it without string-matching.
var errNilEngine = errors.New("hive: worker has nil engine")
