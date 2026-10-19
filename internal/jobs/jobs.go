// Package jobs tracks shell commands running in the background, so a turn can
// end (and the user keep chatting) while they run. A finished job waits in a
// queue until a consumer takes it: the engine folds it into the running turn
// at its next iteration, or the UI starts a new turn when the engine is idle.
// Either way the model never has to sleep or poll.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/elhenro/bee/internal/tools"
)

const (
	// capture window per job, same shape as the foreground shell tool: head and
	// tail kept, middle dropped.
	captureHead = 256 << 10
	captureTail = 256 << 10

	// how long Close waits for killed process groups to be reaped
	closeGrace = 3 * time.Second
)

// State is where a job is in its life.
type State string

const (
	Running  State = "running"
	Exited   State = "exited"
	Killed   State = "killed"
	TimedOut State = "timed out"
)

// Info is a point-in-time copy of a job. Output is filled by Get and TakeDone
// only; List leaves it empty.
type Info struct {
	ID      int
	Label   string
	State   State
	Exit    int
	Started time.Time
	Ended   time.Time // zero while running
	Output  string
}

// Elapsed is the run time so far, or the total once the job ended.
func (i Info) Elapsed() time.Duration {
	if i.Ended.IsZero() {
		return time.Since(i.Started)
	}
	return i.Ended.Sub(i.Started)
}

// Build creates the command for one job. ctx ends the process group (kill,
// timeout, Close); out is where stdout and stderr go. The command must come
// from exec.CommandContext(ctx, ...) and should call tools.GroupKill.
type Build func(ctx context.Context, out io.Writer) *exec.Cmd

type entry struct {
	info   Info
	out    *tools.CapBuffer
	cancel context.CancelFunc
	killed bool // stopped on request: no completion notice is queued
}

// Manager owns the jobs of one bee process.
type Manager struct {
	mu    sync.Mutex
	ctx   context.Context
	stop  context.CancelFunc
	wg    sync.WaitGroup
	next  int
	jobs  map[int]*entry
	fresh []int // finished, not yet taken
	wake  chan struct{}
}

// New returns an empty manager. Close it on exit: that kills what still runs.
func New() *Manager {
	ctx, stop := context.WithCancel(context.Background())
	return &Manager{ctx: ctx, stop: stop, jobs: map[int]*entry{}, wake: make(chan struct{}, 1)}
}

// Spawn starts a job and returns its id at once. timeout <= 0 means none. A
// start failure comes back as the error, no job is recorded.
func (m *Manager) Spawn(label string, timeout time.Duration, build Build) (int, error) {
	if m.ctx.Err() != nil {
		return 0, errors.New("jobs: manager closed")
	}
	var ctx context.Context
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(m.ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(m.ctx)
	}
	out := tools.NewCapBuffer(captureHead, captureTail)
	cmd := build(ctx, out)
	if err := cmd.Start(); err != nil {
		cancel()
		return 0, err
	}
	m.mu.Lock()
	m.next++
	e := &entry{
		info:   Info{ID: m.next, Label: label, State: Running, Started: time.Now()},
		out:    out,
		cancel: cancel,
	}
	m.jobs[e.info.ID] = e
	m.mu.Unlock()

	m.wg.Add(1)
	go m.reap(ctx, e, cmd)
	return e.info.ID, nil
}

func (m *Manager) reap(ctx context.Context, e *entry, cmd *exec.Cmd) {
	defer m.wg.Done()
	// the exit code and ctx say how it ended; a backgrounded grandchild holding
	// the pipe open (exec.ErrWaitDelay) is still a clean exit of the command.
	_ = cmd.Wait()
	ctxErr := ctx.Err()
	e.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()
	e.info.Ended = time.Now()
	e.info.Exit = -1
	if cmd.ProcessState != nil {
		e.info.Exit = cmd.ProcessState.ExitCode()
	}
	switch {
	case e.killed:
		e.info.State = Killed
	case errors.Is(ctxErr, context.DeadlineExceeded):
		e.info.State = TimedOut
	case ctxErr != nil:
		e.info.State = Killed // manager closing
	default:
		e.info.State = Exited
	}
	if e.killed {
		return
	}
	m.fresh = append(m.fresh, e.info.ID)
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Get returns one job with its output so far.
func (m *Manager) Get(id int) (Info, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return Info{}, false
	}
	return snapshot(e, true), true
}

// List returns every job by id, without output.
func (m *Manager) List() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.jobs))
	for _, e := range m.jobs {
		out = append(out, snapshot(e, false))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Kill stops a running job. Its completion is not queued: the caller asked
// for the stop, so no turn needs to hear about it.
func (m *Manager) Kill(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("no job %d", id)
	}
	if e.info.State != Running {
		return fmt.Errorf("job %d already %s", id, e.info.State)
	}
	e.killed = true
	e.cancel()
	return nil
}

// TakeDone returns the jobs that finished since the last call, oldest first,
// with their output, and forgets them as pending.
func (m *Manager) TakeDone() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, 0, len(m.fresh))
	for _, id := range m.fresh {
		out = append(out, snapshot(m.jobs[id], true))
	}
	m.fresh = nil
	return out
}

// Pending counts finished jobs nobody took yet.
func (m *Manager) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.fresh)
}

// Running counts jobs still going.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.jobs {
		if e.info.State == Running {
			n++
		}
	}
	return n
}

// Active reports whether anything still runs or waits to be taken.
func (m *Manager) Active() bool { return m.Running() > 0 || m.Pending() > 0 }

// Wake fires (coalesced) whenever a job finishes. Check Pending after it.
func (m *Manager) Wake() <-chan struct{} { return m.wake }

// Close kills every running job and waits briefly for the process groups to
// be reaped, so none outlives bee. Safe on nil.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.stop()
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(closeGrace):
	}
}

func snapshot(e *entry, withOutput bool) Info {
	in := e.info
	if withOutput {
		in.Output = e.out.String()
	}
	return in
}
