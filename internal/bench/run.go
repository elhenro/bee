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
	BeeBin   string        // path to the bee binary under test
	Provider string        // --provider passthrough (empty = inherit config)
	Model    string        // --model passthrough (empty = inherit config)
	Label    string        // tags the result set (config-variant identifier)
	Weights  Weights       // scoring blend
	Timeout  time.Duration // per-task wall-clock cap
	ExtraEnv []string      // extra env (offline tests inject the scripted provider here)
}

// TaskResult is one task's full outcome.
type TaskResult struct {
	ID        string        `json:"id"`
	Score     float64       `json:"score"`
	Dims      Dims          `json:"dims"`
	Succeeded bool          `json:"succeeded"`
	Metrics   RunMetrics    `json:"metrics"`
	Checks    []CheckResult `json:"checks,omitempty"`
	Reason    string        `json:"reason"`
	Err       string        `json:"error,omitempty"`
}

// SuiteResult is the full scoreboard.
type SuiteResult struct {
	Label     string       `json:"label"`
	Aggregate float64      `json:"aggregate"`
	DimMeans  Dims         `json:"dim_means"`
	Tasks     []TaskResult `json:"tasks"`
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
	res := SuiteResult{Label: opt.Label}
	for _, t := range tasks {
		res.Tasks = append(res.Tasks, runTask(ctx, t, opt))
	}
	res.Aggregate, res.DimMeans = aggregate(res.Tasks)
	return res, nil
}

func runTask(ctx context.Context, t Task, opt Options) TaskResult {
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
	return tr
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

func aggregate(tasks []TaskResult) (float64, Dims) {
	if len(tasks) == 0 {
		return 0, Dims{}
	}
	var total, su, fo, ef float64
	for _, t := range tasks {
		total += t.Score
		su += t.Dims.Success
		fo += t.Dims.Format
		ef += t.Dims.Efficiency
	}
	n := float64(len(tasks))
	return total / n, Dims{Success: su / n, Format: fo / n, Efficiency: ef / n}
}
