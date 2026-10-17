package cron

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Scheduler fires registered jobs on their schedule, gating each run by its
// active-hours window and serializing runs that share a resource key.
//
// Scheduling decisions are driven entirely by the injected Clock and Tick's
// explicit time argument, so tests advance time without sleeping. Start
// additionally polls on a real ticker at Interval; callers that want full
// control may call Tick directly instead of Start.
type Scheduler struct {
	mu       sync.Mutex
	jobs     []*Job
	queue    *Queue
	runner   Runner
	clock    Clock
	interval time.Duration
	logf     func(string, ...any)

	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
	wg      sync.WaitGroup
}

// New builds a scheduler. A nil clock uses the wall clock; a nil runner is a
// no-op. Jobs normally come from a Registry.
func New(clock Clock, runner Runner, jobs []*Job) *Scheduler {
	if clock == nil {
		clock = realClock{}
	}
	if runner == nil {
		runner = RunnerFunc(func(context.Context, *Job) error { return nil })
	}
	s := &Scheduler{
		jobs:     append([]*Job(nil), jobs...),
		queue:    NewQueue(),
		runner:   runner,
		clock:    clock,
		interval: time.Minute,
	}
	now := clock.Now()
	for _, j := range s.jobs {
		if j.nextDue.IsZero() {
			j.nextDue = firstDue(j.Schedule, now)
		}
	}
	return s
}

// firstDue seeds a job's first run from its schedule, not from process
// start: a job fires immediately only when the current minute matches, so a
// yearly job is not run on every restart.
func firstDue(sched *Schedule, now time.Time) time.Time {
	if next := sched.Next(now.Add(-time.Minute)); !next.IsZero() {
		return next
	}
	return now.AddDate(100, 0, 0)
}

// SetInterval overrides the Start poll interval. Values <= 0 are ignored.
func (s *Scheduler) SetInterval(d time.Duration) {
	if d > 0 {
		s.interval = d
	}
}

// SetLogger routes deferral and error notes. Call it before Start.
func (s *Scheduler) SetLogger(f func(string, ...any)) { s.logf = f }

// Jobs returns the scheduler's jobs.
func (s *Scheduler) Jobs() []*Job { return append([]*Job(nil), s.jobs...) }

// Queue exposes the busy queue so callers can serialize work that shares a
// resource key with a scheduled job.
func (s *Scheduler) Queue() *Queue { return s.queue }

// Start launches the polling loop. With no jobs it returns without starting
// any goroutine, so an empty config leaves the scheduler idle.
func (s *Scheduler) Start(parent context.Context) {
	s.mu.Lock()
	if s.started || len(s.jobs) == 0 {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.ctx, s.cancel = context.WithCancel(parent)
	s.done = make(chan struct{})
	ctx, done := s.ctx, s.done
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(done)
		s.loop(ctx)
	}()
}

// Tick evaluates every job at time now, launching the ones that are due and
// inside their active-hours window. A job that is due but outside the window
// is deferred — its due time is not advanced — and fires once the window
// opens. It returns the number of runs launched.
func (s *Scheduler) Tick(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		s.ctx = context.Background()
	}
	fired := 0
	for _, j := range s.jobs {
		if j.running || !j.due(now) {
			continue
		}
		if !j.Window.In(now) {
			s.log("cron: deferred %s until %s (outside active hours)",
				j.Name, j.Window.NextOpen(now).Format(time.RFC3339))
			continue
		}
		j.running = true
		if next := j.Schedule.Next(now); next.IsZero() {
			j.nextDue = now.AddDate(100, 0, 0)
		} else {
			j.nextDue = next
		}
		s.launchLocked(j)
		fired++
	}
	return fired
}

// launchLocked starts one job run. The caller holds s.mu.
func (s *Scheduler) launchLocked(j *Job) {
	ctx := s.ctx
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		release, err := s.queue.Acquire(ctx, j.ResourceKey)
		if err != nil {
			s.finish(j, err)
			return
		}
		defer release()
		s.finish(j, runJob(ctx, s.runner, j))
	}()
}

func (s *Scheduler) finish(j *Job, err error) {
	s.mu.Lock()
	j.running = false
	s.mu.Unlock()
	if err != nil {
		s.log("cron: job %s failed: %v", j.Name, err)
	}
}

// Shutdown stops the poll loop and waits for in-flight runs to finish. It is
// safe to call even when Start was never called.
func (s *Scheduler) Shutdown() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	s.wg.Wait()
}

func (s *Scheduler) loop(ctx context.Context) {
	s.Tick(s.clock.Now())
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick(s.clock.Now())
		}
	}
}

func (s *Scheduler) log(format string, args ...any) {
	if s.logf != nil {
		s.logf(format, args...)
	}
}

// runJob calls the runner, converting a panic into an error so a crashed job
// still releases its queue slot.
func runJob(ctx context.Context, r Runner, j *Job) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("cron: job %s panicked: %v", j.Name, p)
		}
	}()
	return r.Run(ctx, j)
}
