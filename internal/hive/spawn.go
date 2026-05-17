// Fan-out orchestrator. Spawns up to MaxConcurrency workers in parallel,
// each driving its own Engine via the Runner contract. Results stream out
// on a channel as workers finish; cancellation propagates through ctx.
package hive

import (
	"context"
	"sync"
	"time"
)

// Pool manages a bounded set of concurrent Workers.
type Pool struct {
	MaxConcurrency int

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

// Submit registers a Worker with the pool. State starts at pending.
// Safe to call before Run; calling after Run is a no-op for that worker
// because the dispatcher has already iterated the snapshot.
func (p *Pool) Submit(w *Worker) {
	if w == nil {
		return
	}
	if w.State == "" {
		w.State = StatePending
	}
	p.mu.Lock()
	p.Workers = append(p.Workers, w)
	p.mu.Unlock()
}

// snapshotWorkers returns a copy of the current worker slice header for
// safe iteration without holding the lock during long-running goroutines.
func (p *Pool) snapshotWorkers() []*Worker {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Worker, len(p.Workers))
	copy(out, p.Workers)
	return out
}

// WorkersSnapshot returns a shallow copy of the Worker pointer slice for
// observers (TUI, tests). The pointed-to Worker structs are still shared,
// so reading State/Started/Ended is a live view.
func (p *Pool) WorkersSnapshot() []*Worker {
	return p.snapshotWorkers()
}

// Run dispatches submitted workers up to MaxConcurrency at a time. Each
// worker's Engine.Run is invoked with a context derived from ctx. Results
// land on the returned channel as workers finish; the channel closes once
// all workers are done (including those skipped by cancellation).
func (p *Pool) Run(ctx context.Context) <-chan Result {
	results := make(chan Result, len(p.Workers))
	workers := p.snapshotWorkers()

	sem := make(chan struct{}, p.MaxConcurrency)
	var wg sync.WaitGroup

	for _, w := range workers {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()

			// honor cancellation before acquiring a slot — avoids running
			// late workers after caller has already given up.
			select {
			case <-ctx.Done():
				p.markCanceled(w)
				results <- canceledResult(w, ctx.Err())
				return
			case sem <- struct{}{}:
			}
			defer func() { <-sem }()

			// recheck after acquire — cancellation may have raced.
			if ctx.Err() != nil {
				p.markCanceled(w)
				results <- canceledResult(w, ctx.Err())
				return
			}

			p.markRunning(w)
			res := runOne(ctx, w)
			p.markEnded(w, res.Err)
			results <- res
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()
	return results
}

// runOne executes a single worker's Engine.Run, capturing timing and the
// final assistant text. A nil Engine is a programmer error and produces
// an error Result rather than panicking. Tests inject a Runner via the
// runWorker hook (see spawn_test.go).
func runOne(ctx context.Context, w *Worker) Result {
	started := time.Now().UTC()
	r := Result{
		WorkerID: w.ID,
		Name:     w.Name,
		Task:     w.Task,
		Started:  started,
	}
	runner := resolveRunner(w)
	if runner == nil {
		r.Err = errNilEngine
		r.Ended = time.Now().UTC()
		return r
	}
	run, err := runner.Run(ctx, w.Task)
	r.Ended = time.Now().UTC()
	r.Final = run.FinalText
	if err != nil {
		r.Err = err
	}
	return r
}

// resolveRunner picks the Runner for a Worker. The default uses w.Engine;
// tests override via runnerHook to inject a stub Runner without modifying
// Worker (types.go is owned by another slice).
func resolveRunner(w *Worker) Runner {
	if runnerHook != nil {
		if r := runnerHook(w); r != nil {
			return r
		}
	}
	if w.Engine == nil {
		return nil
	}
	return w.Engine
}

// runnerHook is a test-only seam. Production leaves it nil.
var runnerHook func(*Worker) Runner

func (p *Pool) markRunning(w *Worker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.State = StateRunning
	w.Started = time.Now().UTC()
}

func (p *Pool) markEnded(w *Worker, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.Ended = time.Now().UTC()
	if err != nil {
		w.State = StateFailed
	} else {
		w.State = StateDone
	}
}

func (p *Pool) markCanceled(w *Worker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w.State = StateCanceled
	if w.Started.IsZero() {
		w.Started = time.Now().UTC()
	}
	w.Ended = time.Now().UTC()
}

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
type nilEngineErr struct{}

func (nilEngineErr) Error() string { return "hive: worker has nil engine" }

var errNilEngine error = nilEngineErr{}
