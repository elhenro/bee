package bench

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/elhenro/bee/internal/types"
)

// Options configures a suite run.
type Options struct {
	BeeBin     string        // path to the bee binary under test
	Provider   string        // --provider passthrough (empty = inherit config)
	Model      string        // --model passthrough (empty = inherit config)
	ConfigFile string        // BEE_CONFIG overlay path (empty = default ~/.bee/config.toml)
	Profile    string        // BEE_PROFILE override (empty = inherit/auto)
	Label      string        // tags the result set (config-variant identifier)
	Weights    Weights       // scoring blend
	Timeout    time.Duration // per-task wall-clock cap
	Runs       int           // repeats per task for variance (0/1 = single shot)
	RolloutDir string        // when set, persist blessed (passed + clean) session jsonl here for fine-tune harvest
	ExtraEnv   []string      // extra env (offline tests inject the scripted provider here)
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
// spread — a single noise figure for the whole run.
type SuiteResult struct {
	Label      string       `json:"label"`
	Runs       int          `json:"runs"`
	Aggregate  float64      `json:"aggregate"`
	MeanSpread float64      `json:"mean_spread,omitempty"`
	DimMeans   Dims         `json:"dim_means"`
	Tasks      []TaskResult `json:"tasks"`
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
	for _, t := range tasks {
		res.Tasks = append(res.Tasks, runTask(ctx, t, opt))
	}
	res.Aggregate, res.MeanSpread, res.DimMeans = aggregate(res.Tasks)
	return res, nil
}

// runTask runs a task opt.Runs times and folds the repeats into one result:
// mean score and dims, score spread (max−min), and the raw samples. The first
// run is the representative for checks/metrics/reason; small models are
// latency-bound so repeats stay serial.
func runTask(ctx context.Context, t Task, opt Options) TaskResult {
	rep := runTaskOnce(ctx, t, opt, 0)
	if opt.Runs <= 1 {
		return rep
	}
	sumScore := rep.Score
	sumSuc, sumFmt, sumEff := rep.Dims.Success, rep.Dims.Format, rep.Dims.Efficiency
	min, max := rep.Score, rep.Score
	succ := boolToInt(rep.Succeeded)
	samples := []float64{rep.Score}
	for i := 1; i < opt.Runs; i++ {
		r := runTaskOnce(ctx, t, opt, i)
		samples = append(samples, r.Score)
		sumScore += r.Score
		sumSuc += r.Dims.Success
		sumFmt += r.Dims.Format
		sumEff += r.Dims.Efficiency
		succ += boolToInt(r.Succeeded)
		if r.Score < min {
			min = r.Score
		}
		if r.Score > max {
			max = r.Score
		}
		if rep.Err == "" && r.Err != "" {
			rep.Err = r.Err // surface any run's error
		}
	}
	n := float64(opt.Runs)
	rep.Score = sumScore / n
	rep.Dims = Dims{Success: sumSuc / n, Format: sumFmt / n, Efficiency: sumEff / n}
	rep.Spread = max - min
	rep.Samples = samples
	rep.Succeeded = 2*succ >= opt.Runs // majority of runs passed
	return rep
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func runTaskOnce(ctx context.Context, t Task, opt Options, runIdx int) TaskResult {
	tr := TaskResult{ID: t.ID}

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
		if out, err := runShell(ctx, t.Setup, sandbox); err != nil {
			tr.Err = fmt.Sprintf("setup failed: %v: %s", err, truncate(out, 200))
			return tr
		}
	}

	stdout, runErr := runBee(ctx, t, opt, sandbox, sessDir)
	stoppedClean := strings.Contains(stdout, "✓ goal achieved")

	msgs, _ := readTranscript(sessDir)
	m := MetricsFromMessages(msgs, stoppedClean)

	var succeeded bool
	if len(t.Checks) > 0 {
		tr.Checks, succeeded = RunChecks(t.Checks, sandbox)
	} else {
		succeeded = stoppedClean // judge verdict surfaced by the goal loop
	}

	tr.Dims, tr.Score = Score(t.Budget, m, succeeded, 0, opt.Weights)
	tr.Succeeded = succeeded
	tr.Metrics = m
	tr.Reason = verdictReason(stdout, succeeded)
	if runErr != nil {
		tr.Err = runErr.Error()
	}

	// persist only blessed rollouts — passed checks, no errored tool calls,
	// stopped cleanly. these are the demonstrations worth cloning. must happen
	// before the deferred sessDir cleanup.
	if opt.RolloutDir != "" && succeeded && m.ErroredCalls == 0 && m.StoppedClean {
		_ = saveRollout(sessDir, opt.RolloutDir, opt.Label, t.ID, runIdx)
	}
	return tr
}

// saveRollout copies the session jsonl out of the doomed sandbox sessions dir
// into a durable dir, named so the harvester can trace it back to label+task+run.
func saveRollout(sessDir, destDir, label, taskID string, runIdx int) error {
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return err
	}
	var src string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			src = filepath.Join(sessDir, e.Name())
			break
		}
	}
	if src == "" {
		return fmt.Errorf("no session jsonl to save")
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

// runBee spawns the real binary against the task's goal in the sandbox.
func runBee(ctx context.Context, t Task, opt Options, sandbox, sessDir string) (string, error) {
	args := []string{"run", "--headless", "--yes"}
	if opt.Provider != "" {
		args = append(args, "--provider", opt.Provider)
	}
	if opt.Model != "" {
		args = append(args, "--model", opt.Model)
	}
	args = append(args, "/goal "+t.Prompt)

	cctx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, opt.BeeBin, args...)
	cmd.Dir = sandbox
	cmd.Env = append(os.Environ(),
		"BEE_SESSIONS_DIR="+sessDir,
	)
	if opt.ConfigFile != "" {
		cmd.Env = append(cmd.Env, "BEE_CONFIG="+opt.ConfigFile)
	}
	if opt.Profile != "" {
		cmd.Env = append(cmd.Env, "BEE_PROFILE="+opt.Profile)
	}
	cmd.Env = append(cmd.Env, opt.ExtraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// runShell runs a task's setup line. It runs from the invocation cwd (not the
// empty sandbox) so relative fixture paths like "cp -r bench/fixtures/x $SANDBOX"
// resolve against the suite. trusted task-suite shell — see checks.go.
func runShell(ctx context.Context, line, sandbox string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(line, "$SANDBOX", sandbox))
	cmd.Env = append(os.Environ(), "SANDBOX="+sandbox)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// readTranscript reads the single session jsonl bee wrote into sessDir.
func readTranscript(sessDir string) ([]types.Message, error) {
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			return parseJSONL(filepath.Join(sessDir, e.Name()))
		}
	}
	return nil, fmt.Errorf("no session written")
}

func verdictReason(stdout string, succeeded bool) string {
	if i := strings.Index(stdout, "✓ goal achieved: "); i >= 0 {
		return strings.TrimSpace(firstLine(stdout[i+len("✓ goal achieved: "):]))
	}
	if i := strings.Index(stdout, "goal: stopped ("); i >= 0 {
		return strings.TrimSpace(firstLine(stdout[i:]))
	}
	if succeeded {
		return "checks passed"
	}
	return "checks failed"
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func aggregate(tasks []TaskResult) (float64, float64, Dims) {
	if len(tasks) == 0 {
		return 0, 0, Dims{}
	}
	var total, spread, su, fo, ef float64
	for _, t := range tasks {
		total += t.Score
		spread += t.Spread
		su += t.Dims.Success
		fo += t.Dims.Format
		ef += t.Dims.Efficiency
	}
	n := float64(len(tasks))
	return total / n, spread / n, Dims{Success: su / n, Format: fo / n, Efficiency: ef / n}
}
