package queen

import (
	"context"
	"time"

	"github.com/elhenro/bee/internal/zzz"
	"github.com/elhenro/bee/internal/zzz/uikit"
)

// Model is the bubbletea supervisor ("queen") that watches N headless worker
// bees. Each worker's zzz.Drive runs in its own goroutine and pushes status
// through a per-worker WorkerUI into the shared msgs channel; the queen never
// touches worker state directly.
type Model struct {
	workers []*workerState
	uis     []*WorkerUI
	width   int
	height  int
	start   time.Time
	tick    int
	nDone   int

	// cancelAll cancels the parent ctx shared by every worker's Drive. The
	// engine honors ctx.Done mid-stream, so calling this aborts even a wedged
	// iteration — the queen's hard kill switch.
	cancelAll context.CancelFunc
	stopArmed bool

	box *uikit.Mailbox
}

// New builds a supervisor for the given runs. cancelAll must cancel the ctx
// every worker Drive shares.
func New(runs []*zzz.Run, cancelAll context.CancelFunc) *Model {
	m := &Model{
		start:     time.Now(),
		cancelAll: cancelAll,
		box:       uikit.NewMailbox(256),
	}
	for i, r := range runs {
		m.workers = append(m.workers, &workerState{
			idx: i, id: r.ID, branch: r.Branch, status: "running",
		})
		m.uis = append(m.uis, &WorkerUI{idx: i, m: m, steer: make(chan zzz.Steer, 16)})
	}
	return m
}

// Worker returns the zzz.UI + Steerable adapter for worker i, handed to that
// worker's Drive call.
func (m *Model) Worker(i int) *WorkerUI { return m.uis[i] }

// Done is called by the orchestrator after a worker's Drive returns.
func (m *Model) Done(i int, r *zzz.Run, err error) { m.box.Send(wDoneMsg{idx: i, run: r, err: err}) }

// Quit silences late sends from worker goroutines during teardown.
func (m *Model) Quit() { m.box.Close() }

// allDone reports whether every worker has reported a terminal status.
func (m *Model) allDone() bool { return m.nDone >= len(m.workers) }

// WorkerUI adapts a worker's zzz.Drive output onto the shared queen model. It
// satisfies zzz.UI and zzz.Steerable.
type WorkerUI struct {
	idx   int
	m     *Model
	steer chan zzz.Steer
}

func (w *WorkerUI) SetIter(n, max int)        { w.m.box.Send(wIterMsg{w.idx, n, max}) }
func (w *WorkerUI) SetPhase(p string)         { w.m.box.Send(wPhaseMsg{w.idx, p}) }
func (w *WorkerUI) SetTokens(t zzz.TokenStat) { w.m.box.Send(wTokensMsg{w.idx, t}) }
func (w *WorkerUI) IncCommits()               { w.m.box.Send(wCommitMsg{w.idx}) }
func (w *WorkerUI) Println(s string)          { w.m.box.Send(wLogMsg{w.idx, s, uikit.LevelFor(s)}) }
func (w *WorkerUI) RenderSummary(r *zzz.Run)  {}
func (w *WorkerUI) Steer() <-chan zzz.Steer   { return w.steer }

// broadcast pushes a steer to every worker (e.g. graceful stop-all). Notes
// are droppable, so a full buffer just skips that worker.
func (m *Model) broadcast(s zzz.Steer) {
	for _, ui := range m.uis {
		select {
		case ui.steer <- s:
		default:
		}
	}
}
