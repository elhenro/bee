package bench

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Options configures a suite run.
type Options struct {
	BeeBin     string        // path to the bee binary under test
	Provider   string        // --provider passthrough (empty = inherit config)
	Model      string        // --model passthrough (empty = inherit config)
	ConfigFile string        // BEE_CONFIG overlay path (empty = default ~/.bee/config.toml)
	Label      string        // tags the result set (config-variant identifier)
	Weights    Weights       // scoring blend
	Timeout    time.Duration // per-task wall-clock cap
	Runs       int           // repeats per task for variance (0/1 = single shot)
	RolloutDir string        // when set, persist blessed (passed + clean) session jsonl here for fine-tune harvest
	ExtraEnv   []string      // extra env (offline tests inject the scripted provider here)
	Progress   io.Writer     // when set, stream timestamped per-task/per-run progress here (nil = silent)
}

// logf writes a timestamped progress line when a progress sink is set. it is
// the only window into a long run: bench buffers everything else until the end.
func (o Options) logf(format string, args ...any) {
	if o.Progress == nil {
		return
	}
	ts := time.Now().Format("15:04:05")
	fmt.Fprintf(o.Progress, "[bench %s] "+format+"\n", append([]any{ts}, args...)...)
}

// TaskResult is one task's full outcome. With Runs>1, Score/Dims are means
// across repeats, Spread is max−min of the per-run scores, and Samples holds
// each run's score so a tuner can judge whether a delta clears the noise.
type TaskResult struct {
	ID        string        `json:"id"`
	Score     float64       `json:"score"`
	Spread    float64       `json:"spread,omitempty"`
	Samples   []float64     `json:"samples,omitempty"`
	Dims      Dims          `json:"dims"`
	Succeeded bool          `json:"succeeded"`
	Metrics   RunMetrics    `json:"metrics"`
	Checks    []CheckResult `json:"checks,omitempty"`
	Reason    string        `json:"reason"`
	Err       string        `json:"error,omitempty"`
}

// SuiteResult is the full scoreboard. MeanSpread is the average per-task score
// spread — a single noise figure for the whole run. Holdout* mirror the main
// fields for the never-tuned slice; they stay zero/empty when no held-out dir
// is present, keeping output backward compatible.
type SuiteResult struct {
	Label            string       `json:"label"`
	Runs             int          `json:"runs"`
	Aggregate        float64      `json:"aggregate"`
	MeanSpread       float64      `json:"mean_spread,omitempty"`
	WallMeanMillis   int64        `json:"wall_mean_millis,omitempty"`
	DimMeans         Dims         `json:"dim_means"`
	Tasks            []TaskResult `json:"tasks"`
	HoldoutAggregate float64      `json:"holdout_aggregate,omitempty"`
	HoldoutDimMeans  Dims         `json:"holdout_dim_means,omitempty"`
	HoldoutTasks     []TaskResult `json:"holdout_tasks,omitempty"`
}

// RunSuite runs every task serially and scores it. Serial keeps results
// reproducible — small models are latency-bound, so parallelism buys little.
func RunSuite(ctx context.Context, tasks []Task, opt Options) (SuiteResult, error) {
	if opt.Timeout == 0 {
		opt.Timeout = 5 * time.Minute
	}
	if (opt.Weights == Weights{}) {
		opt.Weights = DefaultWeights
	}
	if opt.Runs < 1 {
		opt.Runs = 1
	}
	res := SuiteResult{Label: opt.Label, Runs: opt.Runs}
	opt.logf("suite start: %d tasks x %d run(s), timeout %s/task", len(tasks), opt.Runs, opt.Timeout)
	suiteStart := time.Now()
	for i, t := range tasks {
		opt.logf("(%d/%d) %s: start", i+1, len(tasks), t.ID)
		tr := runTask(ctx, t, opt, i+1, len(tasks))
		res.Tasks = append(res.Tasks, tr)
	}
	res.Aggregate, res.MeanSpread, res.DimMeans = aggregate(res.Tasks)
	res.WallMeanMillis = meanWallMillis(res.Tasks)
	opt.logf("suite done: aggregate %.1f in %s", res.Aggregate, time.Since(suiteStart).Round(time.Second))
	return res, nil
}

// runTask runs a task opt.Runs times and folds the repeats into one result:
// mean score and dims, score spread (max−min), and the raw samples. The first
// run is the representative for checks/metrics/reason; small models are
// latency-bound so repeats stay serial.
func runTask(ctx context.Context, t Task, opt Options, taskNum, taskTotal int) TaskResult {
	rep := runTaskOnce(ctx, t, opt, 0, taskNum, taskTotal)
	if opt.Runs <= 1 {
		return rep
	}
	runs := []TaskResult{rep}
	for i := 1; i < opt.Runs; i++ {
		runs = append(runs, runTaskOnce(ctx, t, opt, i, taskNum, taskTotal))
	}
	succ := 0
	for _, r := range runs {
		rep.Samples = append(rep.Samples, r.Score)
		if r.Succeeded {
			succ++
		}
		if rep.Err == "" {
			rep.Err = r.Err // surface any run's error
		}
	}
	rep.Score = mean(runs, func(r TaskResult) float64 { return r.Score })
	rep.Dims = meanDims(runs)
	rep.Metrics.WallMillis = meanWallMillis(runs)
	rep.Spread = slices.Max(rep.Samples) - slices.Min(rep.Samples)
	rep.Succeeded = 2*succ >= opt.Runs // at least half the runs passed (ties count as success)
	return rep
}

func runTaskOnce(ctx context.Context, t Task, opt Options, runIdx, taskNum, taskTotal int) TaskResult {
	tr := TaskResult{ID: t.ID}
	runStart := time.Now()

	sandbox, err := os.MkdirTemp("", "bee-bench-sandbox-")
	if err != nil {
		tr.Err = "mkdir sandbox: " + err.Error()
		return tr
	}
	defer os.RemoveAll(sandbox)
	sessDir, err := os.MkdirTemp("", "bee-bench-sess-")
	if err != nil {
		tr.Err = "mkdir sessions: " + err.Error()
		return tr
	}
	defer os.RemoveAll(sessDir)

	if t.Setup != "" {
		if out, err := runShell(ctx, t.Setup, sandbox, opt.Timeout); err != nil {
			tr.Err = fmt.Sprintf("setup failed: %v: %s", err, truncate(out, 200))
			return tr
		}
	}

	beeHome, err := os.MkdirTemp("", "bee-bench-home-")
	if err != nil {
		tr.Err = "mkdir bee home: " + err.Error()
		return tr
	}
	defer os.RemoveAll(beeHome)

	stderr, runErr := runBee(ctx, t, opt, sandbox, sessDir, beeHome)
	stoppedClean := goalAchieved(stderr)

	msgs, _ := readTranscript(sessDir)
	m := MetricsFromMessages(msgs, stoppedClean)

	// surface the final assistant text into the sandbox so abstain/refusal tasks
	// can assert on it with the same grep machinery as any file check. only when
	// a check asks for it — see taskWantsFinalMessage.
	if taskWantsFinalMessage(t) {
		writeFinalMessage(sandbox, msgs)
	}

	var succeeded bool
	if len(t.Checks) > 0 {
		tr.Checks, succeeded = RunChecks(t.Checks, sandbox, opt.Timeout)
	} else {
		succeeded = stoppedClean // judge verdict surfaced by the goal loop
	}

	// capture wall-clock before any rounding so cold-start deltas survive (the
	// log line below rounds to whole seconds, which would quantize them away).
	wall := time.Since(runStart)
	m.WallMillis = wall.Milliseconds()

	tr.Dims, tr.Score = Score(t.Budget, m, succeeded, opt.Weights)
	tr.Succeeded = succeeded
	tr.Metrics = m
	tr.Reason = verdictReason(stderr, succeeded)
	if runErr != nil {
		tr.Err = runErr.Error()
	}

	// persist only blessed rollouts — passed checks, no errored tool calls,
	// stopped cleanly. these are the demonstrations worth cloning. must happen
	// before the deferred sessDir cleanup.
	if opt.RolloutDir != "" && succeeded && m.ErroredCalls == 0 && m.StoppedClean {
		_ = saveRollout(sessDir, opt.RolloutDir, opt.Label, t.ID, runIdx)
	}

	dur := wall.Round(time.Second)
	note := ""
	if ctx.Err() == nil && runErr != nil && dur >= opt.Timeout {
		note = " TIMEOUT"
	}
	opt.logf("(%d/%d) %s: run %d done score %.1f (suc=%.0f fmt=%.0f eff=%.0f) turns=%d tools=%d err=%d clean=%t in %s%s",
		taskNum, taskTotal, t.ID, runIdx+1, tr.Score,
		tr.Dims.Success, tr.Dims.Format, tr.Dims.Efficiency,
		m.Turns, m.ToolCalls, m.ErroredCalls, m.StoppedClean, dur, note)
	return tr
}

// saveRollout copies the session jsonl out of the doomed sandbox sessions dir
// into a durable dir, named so the harvester can trace it back to label+task+run.
func saveRollout(sessDir, destDir, label, taskID string, runIdx int) error {
	src, err := sessionFile(sessDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if label == "" {
		label = "run"
	}
	name := fmt.Sprintf("%s__%s__r%d.jsonl", label, taskID, runIdx)
	return os.WriteFile(filepath.Join(destDir, name), data, 0o644)
}

// meanWallMillis averages per-task wall-clock across the suite. Per-task values
// are already mean-folded over repeats, so this is the suite-wide mean.
func meanWallMillis(tasks []TaskResult) int64 {
	if len(tasks) == 0 {
		return 0
	}
	var sum int64
	for _, t := range tasks {
		sum += t.Metrics.WallMillis
	}
	return sum / int64(len(tasks))
}

func aggregate(tasks []TaskResult) (float64, float64, Dims) {
	return mean(tasks, func(t TaskResult) float64 { return t.Score }),
		mean(tasks, func(t TaskResult) float64 { return t.Spread }),
		meanDims(tasks)
}

// mean averages f over rs, 0 for none.
func mean(rs []TaskResult, f func(TaskResult) float64) float64 {
	if len(rs) == 0 {
		return 0
	}
	sum := 0.0
	for _, r := range rs {
		sum += f(r)
	}
	return sum / float64(len(rs))
}

func meanDims(rs []TaskResult) Dims {
	return Dims{
		Success:    mean(rs, func(r TaskResult) float64 { return r.Dims.Success }),
		Format:     mean(rs, func(r TaskResult) float64 { return r.Dims.Format }),
		Efficiency: mean(rs, func(r TaskResult) float64 { return r.Dims.Efficiency }),
	}
}
