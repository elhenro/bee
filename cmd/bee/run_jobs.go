package main

import (
	"context"
	"fmt"
	"os"
	"regexp"

	"github.com/elhenro/bee/internal/config"
	"github.com/elhenro/bee/internal/jobs"
	"github.com/elhenro/bee/internal/loop"
	"github.com/elhenro/bee/internal/tools"
	"github.com/elhenro/bee/internal/tools/job"
)

// buildOpt tweaks tool registry construction without widening every builder
// signature. Only entry points that can consume job results pass one.
type buildOpt func(*buildOpts)

type buildOpts struct {
	jobs    *jobs.Manager
	writeRe *regexp.Regexp
}

// withJobs turns on background commands: bash gains `background` and the job
// tool is registered. Pass it only for an engine whose Jobs is the same manager,
// or finished jobs would never reach the model.
func withJobs(m *jobs.Manager) buildOpt { return func(o *buildOpts) { o.jobs = m } }

// withWriteFilter restricts the mutation tools to paths matching re (nil = no
// filter, the same as omitting the option).
func withWriteFilter(re *regexp.Regexp) buildOpt { return func(o *buildOpts) { o.writeRe = re } }

func collectBuildOpts(opts []buildOpt) buildOpts {
	var o buildOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}

// appendJobTool registers the job tool next to a job-aware bash.
func appendJobTool(all []tools.Tool, m *jobs.Manager) []tools.Tool {
	if m == nil {
		return all
	}
	return append(all, job.New(m))
}

// headlessJobs returns a manager for a plain `bee run`, nil for run shapes that
// end without waiting on jobs (bg loop, skill, goal): there a finished job
// would have nobody to tell.
func headlessJobs(bgLoop bool, skill, msg string) *jobs.Manager {
	if _, isGoal := parseGoalMessage(msg); bgLoop || skill != "" || isGoal {
		return nil
	}
	return jobs.New()
}

// awaitJobs keeps a headless run alive while background jobs run: each finished
// batch becomes another turn, so the model can act on it. Returns when nothing
// runs and nothing waits, on error, or when ctx is cancelled (Ctrl-C), which
// then kills the jobs on the way out.
func awaitJobs(ctx context.Context, eng *loop.Engine, cfg config.Config, res loop.RunResult, jsonOut bool) (loop.RunResult, error) {
	for eng.Jobs.Active() {
		if eng.Jobs.Pending() == 0 {
			select {
			case <-eng.Jobs.Wake():
			case <-ctx.Done():
				return res, ctx.Err()
			}
			continue
		}
		text, _, ok := eng.JobNotice()
		if !ok {
			continue
		}
		fmt.Fprintln(os.Stderr, "bee: background job finished, continuing…")
		if !jsonOut {
			fmt.Fprintln(os.Stdout)
		}
		eng.InitialMessages = loop.StripDanglingToolUse(res.Messages)
		runCtx, cancel := turnContext(ctx, cfg.Watchdog)
		next, err := eng.Run(runCtx, text)
		cancel()
		res = next
		if err != nil {
			return res, err
		}
	}
	return res, nil
}
