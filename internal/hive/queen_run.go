package hive

import (
	"context"
	"fmt"
	"time"
)

// subResult is the Result skeleton for sub-task idx.
func subResult(idx int, st SubTask) Result {
	return Result{WorkerID: fmt.Sprintf("w%d", idx), Name: fmt.Sprintf("worker-%d", idx), Task: st.Task}
}

// runSub drives one sub-task on a runner: attaches the pheromone tail (when
// the runner can carry one), marks the row running, runs, builds the exit
// report and marks the row finished so siblings see the outcome next turn.
// Both dispatch paths share it; the caller owns per-engine locking. A runner
// panic becomes the sub-task's error, so one bee cannot take the swarm down.
func (q *Queen) runSub(ctx context.Context, idx int, st SubTask, r Runner) (res Result) {
	if st.Role == "" {
		st.Role = RoleBuilder // legacy string-array plans carry no role
	}
	if t, ok := r.(Tailed); ok {
		t.SetPromptTail(q.Pheromone.Tail(idx, st))
	}
	q.Pheromone.Start(idx)
	fire2(q.Hooks.OnWorkerStart, idx, st)
	res = subResult(idx, st)
	res.Started = time.Now().UTC()
	defer func() {
		if p := recover(); p != nil {
			workerPanicked(&res, p)
			q.Pheromone.Finish(idx, res)
		}
	}()
	out, err := r.Run(ctx, st.Task)
	res.Ended = time.Now().UTC()
	if err != nil {
		res.Err = err
	} else {
		res.Final = out.FinalText
	}
	res.Report = reportFor(ctx, r, out, err)
	q.Pheromone.Finish(idx, res)
	return res
}
